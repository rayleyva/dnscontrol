package models

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/StackExchange/dnscontrol/pkg/transform"
	"github.com/miekg/dns/dnsutil"
	"golang.org/x/net/idna"
)

// DefaultTTL is applied to any DNS record without an explicit TTL.
const DefaultTTL = uint32(300)

// DNSConfig describes the desired DNS configuration, usually loaded from dnsconfig.js.
type DNSConfig struct {
	Registrars   []*RegistrarConfig   `json:"registrars"`
	DNSProviders []*DNSProviderConfig `json:"dns_providers"`
	Domains      []*DomainConfig      `json:"domains"`
}

// FindDomain returns the *DomainConfig for domain query in config.
func (config *DNSConfig) FindDomain(query string) *DomainConfig {
	for _, b := range config.Domains {
		if b.Name == query {
			return b
		}
	}
	return nil
}

// RegistrarConfig describes a registrar.
type RegistrarConfig struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Metadata json.RawMessage `json:"meta,omitempty"`
}

// DNSProviderConfig describes a DNS service provider.
type DNSProviderConfig struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Metadata json.RawMessage `json:"meta,omitempty"`
}

// PostProcessRecords does any post-processing of the downloaded DNS records.
func PostProcessRecords(recs []*RecordConfig) {
	Downcase(recs)
	fixTxt(recs)
}

// Downcase converts all labels and targets to lowercase in a list of RecordConfig.
func Downcase(recs []*RecordConfig) {
	for _, r := range recs {
		r.Name = strings.ToLower(r.Name)
		r.NameFQDN = strings.ToLower(r.NameFQDN)
		switch r.Type {
		case "ANAME", "CNAME", "MX", "NS", "PTR":
			r.Target = strings.ToLower(r.Target)
		case "A", "AAAA", "ALIAS", "CAA", "IMPORT_TRANSFORM", "SRV", "TLSA", "TXT", "SOA", "CF_REDIRECT", "CF_TEMP_REDIRECT":
			// Do nothing.
		default:
			// TODO: we'd like to panic here, but custom record types complicate things.
		}
	}
	return
}

// fixTxt fixes TXT records generated by providers that do not understand CanUseTXTMulti.
func fixTxt(recs []*RecordConfig) {
	for _, r := range recs {
		if r.Type == "TXT" {
			if len(r.TxtStrings) == 0 {
				r.TxtStrings = []string{r.Target}
			}
		}
	}
}

// CheckDomainIntegrity performs sanity checks on a DomainConfig
// and panics if problems are found.
func (dc DomainConfig) CheckDomainIntegrity() {
	// Assert:  dc.Name should not end with "."
	if strings.HasSuffix(dc.Name, ".") {
		panic(fmt.Errorf("domain name %s ends with dot", dc.Name))
	}
	// Assert: RecordConfig.Name and .NameFQDN should match.
	checkNameFQDN(dc.Records, dc.Name)
}

// checkNameFQDN panics if there is a Name/NameFQDN mismatch.
func checkNameFQDN(recs []*RecordConfig, origin string) {
	for _, r := range recs {

		expectedShort := dnsutil.TrimDomainName(r.NameFQDN, origin)
		if r.Name != expectedShort {
			panic(fmt.Errorf("Name/NameFQDN mismatch: short=(%s) but (%s)-(%s)->(%s)", r.Name, r.NameFQDN, origin, expectedShort))
		}
		expectedFQDN := dnsutil.AddOrigin(r.Name, origin)
		if r.NameFQDN != expectedFQDN {
			panic(fmt.Errorf("Name/NameFQDN mismatch: fqdn=(%s) but (%s)+(%s)->(%s)", r.NameFQDN, r.Name, origin, expectedFQDN))
		}
	}
}

// RecordKey represents a resource record in a format used by some systems.
type RecordKey struct {
	Name string
	Type string
}

// Key converts a RecordConfig into a RecordKey.
func (rc *RecordConfig) Key() RecordKey {
	return RecordKey{rc.Name, rc.Type}
}

// Nameserver describes a nameserver.
type Nameserver struct {
	Name   string `json:"name"` // Normalized to a FQDN with NO trailing "."
	Target string `json:"target"`
}

// StringsToNameservers constructs a list of *Nameserver structs using a list of FQDNs.
func StringsToNameservers(nss []string) []*Nameserver {
	nservers := []*Nameserver{}
	for _, ns := range nss {
		nservers = append(nservers, &Nameserver{Name: ns})
	}
	return nservers
}

// DomainConfig describes a DNS domain (tecnically a  DNS zone).
type DomainConfig struct {
	Name          string            `json:"name"` // NO trailing "."
	Registrar     string            `json:"registrar"`
	DNSProviders  map[string]int    `json:"dnsProviders"`
	Metadata      map[string]string `json:"meta,omitempty"`
	Records       Records           `json:"records"`
	Nameservers   []*Nameserver     `json:"nameservers,omitempty"`
	KeepUnknown   bool              `json:"keepunknown,omitempty"`
	IgnoredLabels []string          `json:"ignored_labels,omitempty"`
}

// Copy returns a deep copy of the DomainConfig.
func (dc *DomainConfig) Copy() (*DomainConfig, error) {
	newDc := &DomainConfig{}
	err := copyObj(dc, newDc)
	return newDc, err
}

// Copy returns a deep copy of a RecordConfig.
func (rc *RecordConfig) Copy() (*RecordConfig, error) {
	newR := &RecordConfig{}
	err := copyObj(rc, newR)
	return newR, err
}

// Punycode will convert all records to punycode format.
// It will encode:
// - Name
// - NameFQDN
// - Target (CNAME and MX only)
func (dc *DomainConfig) Punycode() error {
	var err error
	for _, rec := range dc.Records {
		rec.Name, err = idna.ToASCII(rec.Name)
		if err != nil {
			return err
		}
		rec.NameFQDN, err = idna.ToASCII(rec.NameFQDN)
		if err != nil {
			return err
		}
		switch rec.Type { // #rtype_variations
		case "ALIAS", "MX", "NS", "CNAME", "PTR", "SRV", "URL", "URL301", "FRAME", "R53_ALIAS":
			rec.Target, err = idna.ToASCII(rec.Target)
			if err != nil {
				return err
			}
		case "A", "AAAA", "CAA", "TXT", "TLSA":
			// Nothing to do.
		default:
			msg := fmt.Sprintf("Punycode rtype %v unimplemented", rec.Type)
			panic(msg)
			// We panic so that we quickly find any switch statements
			// that have not been updated for a new RR type.
		}
	}
	return nil
}

// CombineMXs will merge the priority into the target field for all mx records.
// Useful for providers that desire them as one field.
func (dc *DomainConfig) CombineMXs() {
	for _, rec := range dc.Records {
		if rec.Type == "MX" {
			if rec.CombinedTarget {
				pm := strings.Join([]string{"CombineMXs: Already collapsed: ", rec.Name, rec.Target}, " ")
				panic(pm)
			}
			rec.Target = fmt.Sprintf("%d %s", rec.MxPreference, rec.Target)
			rec.MxPreference = 0
			rec.CombinedTarget = true
		}
	}
}

// SplitCombinedMxValue splits a combined MX preference and target into
// separate entities, i.e. splitting "10 aspmx2.googlemail.com."
// into "10" and "aspmx2.googlemail.com.".
func SplitCombinedMxValue(s string) (preference uint16, target string, err error) {
	parts := strings.Fields(s)

	if len(parts) != 2 {
		return 0, "", fmt.Errorf("MX value %#v contains too many fields", s)
	}

	n64, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return 0, "", fmt.Errorf("MX preference %#v does not fit into a uint16", parts[0])
	}
	return uint16(n64), parts[1], nil
}

// CombineSRVs will merge the priority, weight, and port into the target field for all srv records.
// Useful for providers that desire them as one field.
func (dc *DomainConfig) CombineSRVs() {
	for _, rec := range dc.Records {
		if rec.Type == "SRV" {
			if rec.CombinedTarget {
				pm := strings.Join([]string{"CombineSRVs: Already collapsed: ", rec.Name, rec.Target}, " ")
				panic(pm)
			}
			rec.Target = fmt.Sprintf("%d %d %d %s", rec.SrvPriority, rec.SrvWeight, rec.SrvPort, rec.Target)
			rec.CombinedTarget = true
		}
	}
}

// SplitCombinedSrvValue splits a combined SRV priority, weight, port and target into
// separate entities, some DNS providers want "5" "10" 15" and "foo.com.",
// while other providers want "5 10 15 foo.com.".
func SplitCombinedSrvValue(s string) (priority, weight, port uint16, target string, err error) {
	parts := strings.Fields(s)

	if len(parts) != 4 {
		return 0, 0, 0, "", fmt.Errorf("SRV value %#v contains too many fields", s)
	}

	priorityconv, err := strconv.ParseInt(parts[0], 10, 16)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("Priority %#v does not fit into a uint16", parts[0])
	}
	weightconv, err := strconv.ParseInt(parts[1], 10, 16)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("Weight %#v does not fit into a uint16", parts[0])
	}
	portconv, err := strconv.ParseInt(parts[2], 10, 16)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("Port %#v does not fit into a uint16", parts[0])
	}
	return uint16(priorityconv), uint16(weightconv), uint16(portconv), parts[3], nil
}

// CombineCAAs will merge the tags and flags into the target field for all CAA records.
// Useful for providers that desire them as one field.
func (dc *DomainConfig) CombineCAAs() {
	for _, rec := range dc.Records {
		if rec.Type == "CAA" {
			if rec.CombinedTarget {
				pm := strings.Join([]string{"CombineCAAs: Already collapsed: ", rec.Name, rec.Target}, " ")
				panic(pm)
			}
			rec.Target = rec.Content()
			rec.CombinedTarget = true
		}
	}
}

// SplitCombinedCaaValue parses a string listing the parts of a CAA record into its components.
func SplitCombinedCaaValue(s string) (tag string, flag uint8, value string, err error) {

	splitData := strings.SplitN(s, " ", 3)
	if len(splitData) != 3 {
		err = fmt.Errorf("Unexpected data for CAA record returned by Vultr")
		return
	}

	lflag, err := strconv.ParseUint(splitData[0], 10, 8)
	if err != nil {
		return
	}
	flag = uint8(lflag)

	tag = splitData[1]

	value = splitData[2]
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}
	if strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`) {
		value = value[1 : len(value)-1]
	}
	return
}

func copyObj(input interface{}, output interface{}) error {
	buf := &bytes.Buffer{}
	enc := gob.NewEncoder(buf)
	dec := gob.NewDecoder(buf)
	if err := enc.Encode(input); err != nil {
		return err
	}
	return dec.Decode(output)
}

// HasRecordTypeName returns True if there is a record with this rtype and name.
func (dc *DomainConfig) HasRecordTypeName(rtype, name string) bool {
	for _, r := range dc.Records {
		if r.Type == rtype && r.Name == name {
			return true
		}
	}
	return false
}

// Filter removes all records that don't match the filter f.
func (dc *DomainConfig) Filter(f func(r *RecordConfig) bool) {
	recs := []*RecordConfig{}
	for _, r := range dc.Records {
		if f(r) {
			recs = append(recs, r)
		}
	}
	dc.Records = recs
}

// InterfaceToIP returns an IP address when given a 32-bit value or a string. That is,
// dnsconfig.js output may represent IP addresses as either  a string ("1.2.3.4")
// or as an numeric value (the integer representation of the 32-bit value). This function
// converts either to a net.IP.
func InterfaceToIP(i interface{}) (net.IP, error) {
	switch v := i.(type) {
	case float64:
		u := uint32(v)
		return transform.UintToIP(u), nil
	case string:
		if ip := net.ParseIP(v); ip != nil {
			return ip, nil
		}
		return nil, fmt.Errorf("%s is not a valid ip address", v)
	default:
		return nil, fmt.Errorf("cannot convert type %s to ip", reflect.TypeOf(i))
	}
}

// Correction is anything that can be run. Implementation is up to the specific provider.
type Correction struct {
	F   func() error `json:"-"`
	Msg string
}
