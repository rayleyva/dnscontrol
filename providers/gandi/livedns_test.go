package gandi

import (
	"testing"

	"github.com/StackExchange/dnscontrol/models"
	"github.com/google/uuid"
	"github.com/prasmussen/gandi-api/live_dns/domain"
	"github.com/prasmussen/gandi-api/live_dns/record"
	"github.com/prasmussen/gandi-api/live_dns/zone"
	"github.com/stretchr/testify/assert"
)

type testRecordManager struct {
	t           testing.TB
	args        []string
	status      *record.Status
	infos       []*record.Info
	info        record.Info
	createCalls int
}

func (r *testRecordManager) Create(recordInfo record.Info, args ...string) (*record.Status, error) {
	assert.Equal(r.t, r.info, recordInfo)
	if r.args != nil {
		assert.Equal(r.t, r.args, args)
	}
	r.createCalls++
	return r.status, nil
}

func (r *testRecordManager) Update(recordInfo record.Info, args ...string) (*record.Status, error) {
	assert.Equal(r.t, r.info, recordInfo)
	if r.args != nil {
		assert.Equal(r.t, r.args, args)
	}
	return r.status, nil
}

func (r *testRecordManager) List(args ...string) ([]*record.Info, error) {
	if r.args != nil {
		assert.Equal(r.t, r.args, args)
	}
	return r.infos, nil
}

func (r *testRecordManager) Delete(args ...string) error {
	assert.Equal(r.t, r.args, args)
	return nil
}

type testDomainManager struct {
	t             testing.TB
	domain        string
	domainInfo    *domain.Info
	recordManager record.Manager
}

func (d *testDomainManager) Info(domain string) (*domain.Info, error) {
	assert.Equal(d.t, d.domain, domain)
	return d.domainInfo, nil
}

func (d *testDomainManager) Records(domain string) record.Manager {
	return d.recordManager
}

type testZoneManager struct {
	t             testing.TB
	byUUIDInfos   []*zone.Info
	createInfos   zone.Info
	createStatus  *zone.CreateStatus
	setDomain     string
	setZoneInfo   zone.Info
	setStatus     *zone.Status
	recordInfos   zone.Info
	recordManager record.Manager
	createCalls   int
	setCalls      int
}

func (z *testZoneManager) InfoByUUID(uuid.UUID) (*zone.Info, error) {
	r := z.byUUIDInfos[0]
	z.byUUIDInfos = z.byUUIDInfos[1:]
	return r, nil
}

func (z *testZoneManager) Create(infos zone.Info) (*zone.CreateStatus, error) {
	assert.Equal(z.t, z.createInfos, infos)
	z.createCalls++
	return z.createStatus, nil
}

func (z *testZoneManager) Set(domain string, infos zone.Info) (*zone.Status, error) {
	assert.Equal(z.t, z.setDomain, domain)
	assert.Equal(z.t, z.setZoneInfo, infos)
	z.setCalls++
	return z.setStatus, nil
}

func (z *testZoneManager) Records(infos zone.Info) record.Manager {
	assert.Equal(z.t, z.recordInfos, infos)
	return z.recordManager
}

func TestDomainCorrectionNewDomain(t *testing.T) {

	domainRecordManager := testRecordManager{
		infos: []*record.Info{
			{
				Name:   "www",
				Type:   "A",
				TTL:    500,
				Values: []string{"127.0.0.1", "127.1.0.1"},
			},
		},
		args: nil,
		t:    t,
	}
	model := []*models.RecordConfig{
		{
			NameFQDN: "www.example.com",
			Name:     "www",
			Type:     "A",
			Target:   "127.0.0.1",
			TTL:      500,
		},
		{
			NameFQDN: "www.example.com",
			Name:     "www",
			Type:     "A",
			Target:   "127.1.0.1",
			TTL:      500,
		},
	}
	id := uuid.New()
	domainManager := testDomainManager{
		recordManager: &domainRecordManager,
		domainInfo: &domain.Info{
			InfoExtra: &domain.InfoExtra{
				ZoneUUID: &id,
			},
		},
		domain: "example.com",
	}
	c := liveClient{
		domainManager: &domainManager,
	}
	config := models.DomainConfig{
		Name:    "example.com",
		Records: model,
	}
	corrections, err := c.GetDomainCorrections(&config)
	assert.NoError(t, err)
	assert.Empty(t, corrections)

	// simulate a domain change
	domainRecordManager.infos[0].Values = []string{"127.0.0.1"}
	corrections, err = c.GetDomainCorrections(&config)
	assert.NoError(t, err)
	assert.Len(t, corrections, 1)
	assert.Equal(t, "Setting dns records for example.com:\nA www.example.com 127.0.0.1 500\nA www.example.com 127.1.0.1 500", corrections[0].Msg)

	newRecordManager := testRecordManager{
		info: record.Info{
			Name:   "www",
			Type:   "A",
			TTL:    500,
			Values: []string{"127.0.0.1", "127.1.0.1"},
		},
		t: t,
	}
	newUUID := uuid.New()
	newInfos := zone.Info{
		UUID: &newUUID,
		Name: "test zone",
	}
	oldInfos := zone.Info{
		UUID: &id,
		Name: "test zone",
	}
	zoneManager := testZoneManager{
		t:             t,
		byUUIDInfos:   []*zone.Info{&oldInfos, &newInfos},
		createInfos:   oldInfos,
		createStatus:  &zone.CreateStatus{Status: &zone.Status{Message: "success"}, UUID: &newUUID},
		setDomain:     "example.com",
		setZoneInfo:   newInfos,
		setStatus:     &zone.Status{Message: "success"},
		recordInfos:   newInfos,
		recordManager: &newRecordManager,
	}
	c.zoneManager = &zoneManager
	err = corrections[0].F()
	assert.NoError(t, err)
	assert.Equal(t, 1, zoneManager.createCalls)
	assert.Equal(t, 1, zoneManager.setCalls)
	assert.Equal(t, 1, newRecordManager.createCalls)
}

func TestRecordConfigFromInfo(t *testing.T) {

	for _, data := range []struct {
		info   *record.Info
		config []*models.RecordConfig
	}{
		{
			&record.Info{
				Name:   "www",
				Type:   "A",
				TTL:    500,
				Values: []string{"127.0.0.1", "127.1.0.1"},
			},
			[]*models.RecordConfig{
				&models.RecordConfig{
					NameFQDN: "www.example.com",
					Name:     "www",
					Type:     "A",
					Target:   "127.0.0.1",
					TTL:      500,
				},
				&models.RecordConfig{
					NameFQDN: "www.example.com",
					Name:     "www",
					Type:     "A",
					Target:   "127.1.0.1",
					TTL:      500,
				},
			},
		},
		{
			&record.Info{
				Name:   "www",
				Type:   "TXT",
				TTL:    500,
				Values: []string{"\"test 2\"", "\"test message test message test message\""},
			},
			[]*models.RecordConfig{
				&models.RecordConfig{
					NameFQDN:   "www.example.com",
					Name:       "www",
					Type:       "TXT",
					Target:     "\"test 2\" \"test message test message test message\"",
					TxtStrings: []string{"test 2", "test message test message test message"},
					TTL:        500,
				},
			},
		},
		{
			&record.Info{
				Name: "www",
				Type: "CAA",
				TTL:  500,
				// examples from https://sslmate.com/caa/
				Values: []string{"0 issue \"www.certinomis.com\"", "0 issuewild \"buypass.com\""},
			},
			[]*models.RecordConfig{
				&models.RecordConfig{
					NameFQDN: "www.example.com",
					Name:     "www",
					Type:     "CAA",
					Target:   "www.certinomis.com",
					CaaFlag:  0,
					CaaTag:   "issue",
					TTL:      500,
				},
				&models.RecordConfig{
					NameFQDN: "www.example.com",
					Name:     "www",
					Type:     "CAA",
					Target:   "buypass.com",
					CaaFlag:  0,
					CaaTag:   "issuewild",
					TTL:      500,
				},
			},
		},
		{
			&record.Info{
				Name:   "test",
				Type:   "SRV",
				TTL:    500,
				Values: []string{"20 0 5060 backupbox.example.com."},
			},
			[]*models.RecordConfig{
				&models.RecordConfig{
					NameFQDN:    "test.example.com",
					Name:        "test",
					Type:        "SRV",
					Target:      "backupbox.example.com.",
					SrvPriority: 20,
					SrvWeight:   0,
					SrvPort:     5060,
					TTL:         500,
				},
			},
		},
		{
			&record.Info{
				Name:   "mail",
				Type:   "MX",
				TTL:    500,
				Values: []string{"50 fb.mail.gandi.net.", "10 spool.mail.gandi.net."},
			},
			[]*models.RecordConfig{
				&models.RecordConfig{
					NameFQDN:     "mail.example.com",
					Name:         "mail",
					Type:         "MX",
					MxPreference: 50,
					Target:       "fb.mail.gandi.net.",
					TTL:          500,
				},
				&models.RecordConfig{
					NameFQDN:     "mail.example.com",
					Name:         "mail",
					Type:         "MX",
					MxPreference: 10,
					Target:       "spool.mail.gandi.net.",
					TTL:          500,
				},
			},
		},
	} {
		t.Run("with record type "+data.info.Type, func(t *testing.T) {
			c := liveClient{}
			for _, c := range data.config {
				c.Original = data.info
			}
			t.Run("Convert gandi info to record config", func(t *testing.T) {
				recordConfig := c.recordConfigFromInfo([]*record.Info{data.info}, "example.com")
				assert.Equal(t, data.config, recordConfig)
			})
			t.Run("Convert record config to gandi info", func(t *testing.T) {
				recordInfos, err := c.recordsToInfo(data.config)
				assert.NoError(t, err)
				assert.Equal(t, []*record.Info{data.info}, recordInfos)
			})
		})
	}
}
