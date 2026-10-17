package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gecko-term/gecko/internal/client"
	"github.com/gecko-term/gecko/internal/cloud"
	"github.com/gecko-term/gecko/internal/config"
	"github.com/gecko-term/gecko/internal/proto"
	"github.com/gecko-term/gecko/internal/relay"
)

// Account commands (docs/cloud.md). None of them run unless you type them;
// a machine that never signs in never talks to a relay.

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	relayURL := fs.String("relay", os.Getenv("GECKO_RELAY"), "relay address, e.g. https://relay.example.com")
	account := fs.String("account", "", "account name (default: your user name)")
	recovery := fs.String("recovery", "", "join with the account's recovery code instead of an approval")
	fp := fs.String("fingerprint", "", "the account fingerprint to expect (gecko whoami on a signed-in machine shows it)")
	_ = fs.Parse(args)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if !cfg.CloudEnabled() {
		return errors.New(`cloud features are off in ` + config.Path() + ` ("cloud": {"enabled": false})`)
	}
	if *account == "" {
		if u, err := user.Current(); err == nil {
			*account = strings.ToLower(filepath.Base(u.Username))
		}
	}
	_, err = cloud.Login(cloud.LoginOptions{Relay: *relayURL, Account: *account, Name: cfg.Name,
		Recovery: *recovery, Fingerprint: *fp, Out: os.Stdout})
	if err != nil {
		return err
	}
	c, err := client.Connect(false)
	if err != nil {
		fmt.Println("Start gecko on this machine (just `gecko`) to bring it online in the account.")
		return nil
	}
	defer c.Close()
	if _, err := c.Call(&proto.Msg{T: proto.TCloud, Name: "reload"}); err != nil {
		return fmt.Errorf("signed in, but the running gecko didn't pick it up (%v); restart it with gecko upgrade", err)
	}
	fmt.Println("The gecko service on this machine is now connected to the relay.")
	return nil
}

func cmdLogout() error {
	c, err := client.Connect(false)
	if err != nil {
		if !cloud.SignedIn(cloud.Dir()) {
			return errors.New("not signed in")
		}
		err = cloud.Logout()
	} else {
		defer c.Close()
		_, err = c.Call(&proto.Msg{T: proto.TCloud, Name: "logout"})
	}
	if err == nil {
		fmt.Println("Signed out. This machine left the account and its keys are deleted.")
	}
	return err
}

// cloudState asks the running daemon, or reads the saved account.
func cloudState() (*proto.CloudState, error) {
	if c, err := client.Connect(false); err == nil {
		defer c.Close()
		r, err := c.Call(&proto.Msg{T: proto.TCloud, Name: "status"})
		if err != nil {
			return nil, err
		}
		return r.Cloud, nil
	}
	a, err := cloud.LoadAccount(cloud.Dir())
	if err != nil || a == nil {
		return &proto.CloudState{}, err
	}
	st := &proto.CloudState{SignedIn: true, Account: a.Name, Fingerprint: a.Fingerprint, Relay: a.Relay, Self: a.Device,
		Error: "gecko isn't running here, so this is the saved list"}
	for _, d := range a.List.Devices {
		st.Devices = append(st.Devices, proto.CloudDevice{ID: d.ID, Name: d.Name, OS: d.OS, Kind: d.Kind, Added: d.Added, Self: d.ID == a.Device})
	}
	return st, nil
}

func cmdWhoami() error {
	st, err := cloudState()
	if err != nil {
		return err
	}
	if st == nil || !st.SignedIn {
		if st != nil && st.Notice != "" {
			fmt.Println(st.Notice)
		}
		return errors.New("not signed in (gecko login --relay URL)")
	}
	conn := "connected"
	if !st.Connected {
		conn = "not connected"
	}
	if st.Error != "" {
		conn += ": " + st.Error
	}
	fmt.Printf("account      %s\nfingerprint  %s\nrelay        %s (%s)\n\n", st.Account, st.Fingerprint, st.Relay, conn)
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DEVICE\tKIND\tOS\tSTATUS\tIP\tAPPROVED BY")
	for _, d := range st.Devices {
		status := "offline"
		switch {
		case d.Self:
			status = "this machine"
		case d.Kind == cloud.KindRecovery:
			status = "-"
		case d.Online:
			status = "online"
		case d.LastSeen > 0:
			status = "seen " + ago(d.LastSeen) + " ago"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", d.Name, d.Kind, dash(d.OS), status, dash(d.IP), dash(d.By))
	}
	_ = tw.Flush()
	if len(st.Pending) > 0 {
		fmt.Println("\nWaiting for approval (gecko approve <code shown there>):")
		for _, p := range st.Pending {
			fmt.Printf("  %s (%s, %s), expires in %s\n", p.Name, p.OS, p.IP, time.Until(time.UnixMilli(p.Expires)).Round(time.Second))
		}
	}
	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// cloudDo runs an account action in the running daemon.
func cloudDo(action, arg string) (*proto.CloudState, error) {
	c, err := client.Connect(false)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	r, err := c.Call(&proto.Msg{T: proto.TCloud, Name: action, Data: arg})
	if err != nil {
		return nil, err
	}
	return r.Cloud, nil
}

func cmdApprove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gecko approve <code shown on the new machine>")
	}
	before, _ := cloudState()
	st, err := cloudDo("approve", args[0])
	if err != nil {
		return err
	}
	for _, d := range st.Devices {
		if before == nil || !hasDevice(before, d.ID) {
			fmt.Printf("Approved %s (%s). It joins account %s in a moment.\n", d.Name, d.OS, st.Account)
			return nil
		}
	}
	fmt.Println("Approved.")
	return nil
}

func hasDevice(st *proto.CloudState, id string) bool {
	for _, d := range st.Devices {
		if d.ID == id {
			return true
		}
	}
	return false
}

func cmdDeny(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gecko deny <code>")
	}
	_, err := cloudDo("deny", args[0])
	return err
}

func cmdRevoke(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gecko revoke <machine name or id>")
	}
	if _, err := cloudDo("revoke", args[0]); err != nil {
		return err
	}
	fmt.Printf("Revoked %s. Its connections close as soon as each machine gets the new list.\n", args[0])
	return nil
}

func cmdRelay(args []string) error {
	fs := flag.NewFlagSet("relay", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:7900", "address to listen on")
	data := fs.String("data", filepath.Join(config.Home(), "relay"), "directory for the relay's state (relay.json)")
	cert := fs.String("tls-cert", "", "TLS certificate file (otherwise run behind a TLS proxy)")
	key := fs.String("tls-key", "", "TLS key file")
	trustProxy := fs.Bool("trust-proxy", false, "take client addresses from X-Forwarded-For (behind a reverse proxy)")
	_ = fs.Parse(args)
	s, err := relay.New(*data)
	if err != nil {
		return err
	}
	s.TrustProxy = *trustProxy
	return relay.ListenAndServe(s, *listen, *cert, *key)
}
