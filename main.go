package main // import "github.com/baltimore-sun-data/certinfo"
import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/carlmjohnson/flagext"
)

func main() {
	if err := exec(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

const usage = `Usage of certinfo

    certinfo [options] <host>...

Options:
`

func exec() error {
	verbose := flag.Bool("verbose", false)
	mode := "text"
	flag.Var(flagext.Choice(&mode, "json", "text"), "mode", "output mode: json or text")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}

	port := flag.Int("port", 443, "Port to look for TLS certificates on")
	flag.Parse()
	if !*verbose {
		flag.SetOut(ioutil.Discard)
	}

	returnInfo := make([]hostinfo, flag.NArg())
	errs := []error{}
	for i, host := range flag.Args() {
		returnInfo[i].Host = host
		returnInfo[i].Port = *port
		err := returnInfo[i].getCerts()
		if err != nil {
			errs = append(errs, err)
		}
	}

	switch mode {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		err := enc.Encode(&returnInfo)
		if err != nil {
			errs = append(errs, err)
		}

	case "text":
		t := template.Must(template.New("").Parse(`
{{- range . -}}
Host: {{ .Host }}:{{ .Port }}
Certs:
    {{ range .Certs -}}
    Issuer: {{ .Cert.Issuer.CommonName }}
    Subject: {{ .Cert.Subject.CommonName }}
    Not Before: {{ .Cert.NotBefore.Format "Jan 2, 2006 3:04 PM" }}
    Not After: {{ .Cert.NotAfter.Format "Jan 2, 2006 3:04 PM" }}
    DNS names: {{ range .Cert.DNSNames }}{{ . }} {{ end }}
{{ end }}
{{- end -}}
            `))
		if err := t.Execute(os.Stdout, &returnInfo); err != nil {
			errs = append(errs, err)
		}
	}

	return mergeErrors(errs...)
}

type hostinfo struct {
	Host  string
	Port  int
	Certs []certinfo
}

type certinfo struct {
	Cert *x509.Certificate
}

func (h *hostinfo) getCerts() error {
	log.Printf("connecting to %s:%d", h.Host, h.Port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(
		dialer,
		"tcp",
		h.Host+":"+strconv.Itoa(h.Port),
		&tls.Config{
			InsecureSkipVerify: true,
		})
	if err != nil {
		return err
	}

	defer conn.Close()

	if err := conn.Handshake(); err != nil {
		return err
	}

	pc := conn.ConnectionState().PeerCertificates
	h.Certs = make([]certinfo, 0, len(pc))
	for _, cert := range pc {
		if cert.IsCA {
			continue
		}
		h.Certs = append(h.Certs, certinfo{cert})
	}

	return nil
}

func (c certinfo) MarshalJSON() ([]byte, error) {
	var adaptor = struct {
		Issuer, Subject     string
		NotBefore, NotAfter time.Time
		DNSNames            []string
	}{
		c.Cert.Issuer.CommonName,
		c.Cert.Subject.CommonName,
		c.Cert.NotBefore,
		c.Cert.NotAfter,
		c.Cert.DNSNames,
	}
	return json.Marshal(adaptor)
}

func mergeErrors(errs ...error) error {
	filterErrs := errs[:0]
	for _, err := range errs {
		if err != nil {
			filterErrs = append(filterErrs, err)
		}
	}
	if len(filterErrs) < 1 {
		return nil
	}
	if len(filterErrs) == 1 {
		return filterErrs[0]
	}
	a := make([]string, len(filterErrs))
	for i, err := range filterErrs {
		a[i] = err.Error()
	}
	return fmt.Errorf("%d errors: %s", len(a), strings.Join(a, "; "))
}