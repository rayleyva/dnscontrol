package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/StackExchange/dnscontrol/models"
	"github.com/StackExchange/dnscontrol/pkg/nameservers"
	"github.com/StackExchange/dnscontrol/pkg/normalize"
	"github.com/StackExchange/dnscontrol/providers"
	"github.com/StackExchange/dnscontrol/providers/config"
	"github.com/urfave/cli"
)

var previewCommand = &cli.Command{
	Name:  "preview",
	Usage: "read live configuration and identify changes to be made, without applying them",
	Action: func(ctx *cli.Context) error {
		return exit(Preview(globalPreviewArgs))
	},
	Category: catMain,
	Flags:    globalPreviewArgs.flags(),
}

// PreviewArgs contains all data/flags needed to run preview, independently of CLI
type PreviewArgs struct {
	GetDNSConfigArgs
	GetCredentialsArgs
	FilterArgs
}

func (args *PreviewArgs) flags() []cli.Flag {
	flags := args.GetDNSConfigArgs.flags()
	flags = append(flags, args.GetCredentialsArgs.flags()...)
	flags = append(flags, args.FilterArgs.flags()...)
	return flags
}

var pushCommand = &cli.Command{
	Name:  "push",
	Usage: "identify changes to be made, and perform them",
	Action: func(ctx *cli.Context) error {
		return exit(Push(globalPushArgs))
	},
	Category: catMain,
	Flags:    globalPushArgs.flags(),
}

type PushArgs struct {
	PreviewArgs
	Interactive bool
}

func (args *PushArgs) flags() []cli.Flag {
	flags := globalPushArgs.PreviewArgs.flags()
	flags = append(flags, cli.BoolFlag{
		Name:        "i",
		Destination: &args.Interactive,
		Usage:       "Interactive. Confirm or Exclude each correction before they run",
	})
	return flags
}

var globalPushArgs PushArgs

var globalPreviewArgs PreviewArgs

func Preview(args PreviewArgs) error {
	return run(args, false, false)
}

func Push(args PushArgs) error {
	return run(args.PreviewArgs, true, args.Interactive)
}

// run is the main routine common to preview/push
func run(args PreviewArgs, push bool, interactive bool) error {
	// TODO: make truly CLI independent. Perhaps return results on a channel as they occur
	cfg, err := GetDNSConfig(args.GetDNSConfigArgs)
	if err != nil {
		return err
	}
	errs := normalize.NormalizeAndValidateConfig(cfg)
	if PrintValidationErrors(errs) {
		return fmt.Errorf("Exiting due to validation errors")
	}
	registrars, dnsProviders, nonDefaultProviders, err := InitializeProviders(args.CredsFile, cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Initialized %d registrars and %d dns service providers.\n", len(registrars), len(dnsProviders))
	anyErrors := false
	totalCorrections := 0
DomainLoop:
	for _, domain := range cfg.Domains {
		if !args.shouldRunDomain(domain.Name) {
			continue
		}
		fmt.Printf("******************** Domain: %s\n", domain.Name)
		nsList, err := nameservers.DetermineNameservers(domain, 0, dnsProviders)
		if err != nil {
			log.Fatal(err)
		}
		domain.Nameservers = nsList
		nameservers.AddNSRecords(domain)
		for prov := range domain.DNSProviders {
			dc, err := domain.Copy()
			if err != nil {
				log.Fatal(err)
			}
			shouldrun := args.shouldRunProvider(prov, dc, nonDefaultProviders)
			statusLbl := ""
			if !shouldrun {
				statusLbl = "(skipping)"
			}
			fmt.Printf("----- DNS Provider: %s... %s", prov, statusLbl)
			if !shouldrun {
				fmt.Println()
				continue
			}
			dsp, ok := dnsProviders[prov]
			if !ok {
				log.Fatalf("DSP %s not declared.", prov)
			}
			corrections, err := dsp.GetDomainCorrections(dc)
			if err != nil {
				fmt.Println("ERROR")
				anyErrors = true
				fmt.Printf("Error getting corrections: %s\n", err)
				continue DomainLoop
			}
			totalCorrections += len(corrections)
			plural := "s"
			if len(corrections) == 1 {
				plural = ""
			}
			fmt.Printf("%d correction%s\n", len(corrections), plural)
			anyErrors = printOrRunCorrections(corrections, push, interactive) || anyErrors
		}
		if run := args.shouldRunProvider(domain.Registrar, domain, nonDefaultProviders); !run {
			continue
		}
		fmt.Printf("----- Registrar: %s\n", domain.Registrar)
		reg, ok := registrars[domain.Registrar]
		if !ok {
			log.Fatalf("Registrar %s not declared.", reg)
		}
		if len(domain.Nameservers) == 0 && domain.Metadata["no_ns"] != "true" {
			fmt.Printf("No nameservers declared; skipping registrar. Add {no_ns:'true'} to force.\n")
			continue
		}
		dc, err := domain.Copy()
		if err != nil {
			log.Fatal(err)
		}
		corrections, err := reg.GetRegistrarCorrections(dc)
		if err != nil {
			fmt.Printf("Error getting corrections: %s\n", err)
			anyErrors = true
			continue
		}
		totalCorrections += len(corrections)
		anyErrors = printOrRunCorrections(corrections, push, interactive) || anyErrors
	}
	if os.Getenv("TEAMCITY_VERSION") != "" {
		fmt.Fprintf(os.Stderr, "##teamcity[buildStatus status='SUCCESS' text='%d corrections']", totalCorrections)
	}
	fmt.Printf("Done. %d corrections.\n", totalCorrections)
	if anyErrors {
		return fmt.Errorf("Completed with errors")
	}
	return nil
}

// InitializeProviders takes a creds file path and a DNSConfig object. Creates all providers with the proper types, and returns them.
// nonDefaultProviders is a list of providers that should not be run unless explicitly asked for by flags.
func InitializeProviders(credsFile string, cfg *models.DNSConfig) (registrars map[string]providers.Registrar, dnsProviders map[string]providers.DNSServiceProvider, nonDefaultProviders []string, err error) {
	var providerConfigs map[string]map[string]string
	providerConfigs, err = config.LoadProviderConfigs(credsFile)
	if err != nil {
		return
	}
	nonDefaultProviders = []string{}
	for name, vals := range providerConfigs {
		// add "_exclude_from_defaults":"true" to a provider to exclude it from being run unless
		// -providers=all or -providers=name
		if vals["_exclude_from_defaults"] == "true" {
			nonDefaultProviders = append(nonDefaultProviders, name)
		}
	}
	registrars, err = providers.CreateRegistrars(cfg, providerConfigs)
	if err != nil {
		return
	}
	dnsProviders, err = providers.CreateDsps(cfg, providerConfigs)
	if err != nil {
		return
	}
	return
}

var reader = bufio.NewReader(os.Stdin)

func printOrRunCorrections(corrections []*models.Correction, push bool, interactive bool) (anyErrors bool) {
	anyErrors = false
	if len(corrections) == 0 {
		return anyErrors
	}
	for i, correction := range corrections {
		fmt.Printf("#%d: %s\n", i+1, correction.Msg)
		if push {
			if interactive {
				fmt.Print("Run? (Y/n): ")
				txt, err := reader.ReadString('\n')
				run := true
				if err != nil {
					run = false
				}
				txt = strings.ToLower(strings.TrimSpace(txt))
				if txt != "y" {
					run = false
				}
				if !run {
					fmt.Println("Skipping")
					continue
				}
			}
			err := correction.F()
			if err != nil {
				fmt.Println("FAILURE!", err)
				anyErrors = true
			} else {
				fmt.Println("SUCCESS!")
			}
		}
	}
	return anyErrors
}
