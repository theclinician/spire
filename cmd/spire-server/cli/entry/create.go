package entry

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/spiffe/spire/cmd/spire-server/util"
	"github.com/spiffe/spire/pkg/common/idutil"
	"github.com/spiffe/spire/proto/spire/api/registration"
	"github.com/spiffe/spire/proto/spire/common"

	"golang.org/x/net/context"
)

type CreateConfig struct {
	// Socket path of registration API
	RegistrationUDSPath string

	// Path to an optional data file. If set, other
	// opts will be ignored.
	Path string

	// Type and value are delimited by a colon (:)
	// ex. "unix:uid:1000" or "spiffe_id:spiffe://example.org/foo"
	Selectors StringsFlag

	ParentID string
	SpiffeID string
	TTL      int

	// List of SPIFFE IDs of trust domains the registration entry is federated with
	FederatesWith StringsFlag

	// Whether or not the registration entry is for an "admin" workload
	Admin bool

	// Whether or not the entry is for a downstream SPIRE server
	Downstream bool

	// Whether or not the entry represents a node or group of nodes
	Node bool

	// Expiry of entry
	EntryExpiry int64

	// DNSNames entries for SVIDs based on this entry
	DNSNames StringsFlag

	// IPAddresses entries for SVIDs based on this entry
	IPAddresses StringsFlag
}

// Validate performs basic validation, even on fields that we
// have defaults defined for
func (rc *CreateConfig) Validate() (err error) {
	if rc.RegistrationUDSPath == "" {
		return errors.New("a socket path for registration api is required")
	}

	// If a path is set, we have all we need
	if rc.Path != "" {
		return nil
	}

	if len(rc.Selectors) < 1 {
		return errors.New("at least one selector is required")
	}

	if rc.Node && len(rc.FederatesWith) > 0 {
		return errors.New("node entries can not federate")
	}

	if rc.ParentID == "" && !rc.Node {
		return errors.New("a parent ID is required if the node flag is not set")
	}

	if rc.SpiffeID == "" {
		return errors.New("a SPIFFE ID is required")
	}

	if rc.TTL < 0 {
		return errors.New("a TTL is required")
	}

	// make sure all SPIFFE ID's are well formed
	rc.SpiffeID, err = idutil.NormalizeSpiffeID(rc.SpiffeID, idutil.AllowAny())
	if err != nil {
		return err
	}

	if rc.ParentID != "" {
		rc.ParentID, err = idutil.NormalizeSpiffeID(rc.ParentID, idutil.AllowAny())
		if err != nil {
			return err
		}
	}

	for i := range rc.FederatesWith {
		rc.FederatesWith[i], err = idutil.NormalizeSpiffeID(rc.FederatesWith[i], idutil.AllowAny())
		if err != nil {
			return err
		}
	}

	return nil
}

type CreateCLI struct{}

func (CreateCLI) Synopsis() string {
	return "Creates registration entries"
}

func (c CreateCLI) Help() string {
	_, err := c.newConfig([]string{"-h"})
	return err.Error()
}

func (c CreateCLI) Run(args []string) int {
	ctx := context.Background()

	config, err := c.newConfig(args)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}

	if err = config.Validate(); err != nil {
		fmt.Println(err.Error())
		return 1
	}

	var entries []*common.RegistrationEntry
	if config.Path != "" {
		entries, err = c.parseFile(config.Path)
	} else {
		entries, err = c.parseConfig(config)
	}
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}

	cl, err := util.NewRegistrationClient(config.RegistrationUDSPath)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}

	err = c.registerEntries(ctx, cl, entries)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}

	return 0
}

// parseConfig builds a registration entry from the given config
func (c CreateCLI) parseConfig(config *CreateConfig) ([]*common.RegistrationEntry, error) {
	e := &common.RegistrationEntry{
		ParentId:    config.ParentID,
		SpiffeId:    config.SpiffeID,
		Ttl:         int32(config.TTL),
		Downstream:  config.Downstream,
		EntryExpiry: config.EntryExpiry,
		DnsNames:    config.DNSNames,
		IPAddresses: config.IPAddresses,
	}

	// If the node flag is set, then set the Parent ID to the server's expected SPIFFE ID
	if config.Node {
		id, err := idutil.ParseSpiffeID(e.SpiffeId, idutil.AllowAny())
		if err != nil {
			return nil, err
		}

		id.Path = "/spire/server"
		e.ParentId = id.String()
	}

	selectors := []*common.Selector{}
	for _, s := range config.Selectors {
		cs, err := parseSelector(s)
		if err != nil {
			return nil, err
		}

		selectors = append(selectors, cs)
	}

	e.Selectors = selectors
	e.FederatesWith = config.FederatesWith
	e.Admin = config.Admin
	return []*common.RegistrationEntry{e}, nil
}

func (CreateCLI) parseFile(path string) ([]*common.RegistrationEntry, error) {
	entries := &common.RegistrationEntries{}

	dat, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(dat, &entries); err != nil {
		return nil, err
	}
	return entries.Entries, nil
}

func (CreateCLI) registerEntries(ctx context.Context, c registration.RegistrationClient, entries []*common.RegistrationEntry) error {
	for _, e := range entries {
		id, err := c.CreateEntry(ctx, e)
		if err != nil {
			fmt.Println("FAILED to create the following entry:")
			printEntry(e)
			return err
		}

		e.EntryId = id.Id
		printEntry(e)
	}

	return nil
}

func (CreateCLI) newConfig(args []string) (*CreateConfig, error) {
	f := flag.NewFlagSet("entry create", flag.ContinueOnError)
	c := &CreateConfig{}

	f.StringVar(&c.RegistrationUDSPath, "registrationUDSPath", util.DefaultSocketPath, "Registration API UDS path")
	f.StringVar(&c.ParentID, "parentID", "", "The SPIFFE ID of this record's parent")
	f.StringVar(&c.SpiffeID, "spiffeID", "", "The SPIFFE ID that this record represents")
	f.IntVar(&c.TTL, "ttl", 3600, "The lifetime, in seconds, for SVIDs issued based on this registration entry")

	f.StringVar(&c.Path, "data", "", "Path to a file containing registration JSON (optional)")

	f.Var(&c.Selectors, "selector", "A colon-delimited type:value selector. Can be used more than once")
	f.Var(&c.FederatesWith, "federatesWith", "SPIFFE ID of a trust domain to federate with. Can be used more than once")

	f.BoolVar(&c.Node, "node", false, "If set, this entry will be applied to matching nodes rather than workloads")
	f.BoolVar(&c.Admin, "admin", false, "If set, the SPIFFE ID in this entry will be granted access to the Registration API")
	f.BoolVar(&c.Downstream, "downstream", false, "A boolean value that, when set, indicates that the entry describes a downstream SPIRE server")
	f.Int64Var(&c.EntryExpiry, "entryExpiry", 0, "An expiry, from epoch in seconds, for the resulting registration entry to be pruned")

	f.Var(&c.DNSNames, "dns", "A DNS name that will be included in SVIDs issued based on this entry, where appropriate. Can be used more than once")
	f.Var(&c.IPAddresses, "ip", "An IP Address that will be included in SVIDs issued based on this entry, where appropriate. Can be used more than once")

	return c, f.Parse(args)
}
