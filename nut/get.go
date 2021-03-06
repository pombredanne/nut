package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/AlekSi/nut"
)

var (
	cmdGet = &Command{
		Run:       runGet,
		UsageLine: "get [-p prefix] [-v] [name, import path or URL]",
		Short:     "download and install nut and dependencies",
	}

	getP string
	getV bool
)

func init() {
	cmdGet.Long = `
Downloads and installs nut and dependencies from http://gonuts.io/ or specified URL.

Examples:
    nut install aleksi/nut
    nut install aleksi/nut/0.2.0
    nut install gonuts.io/aleksi/nut
    nut install gonuts.io/aleksi/nut/0.2.0
    nut install http://gonuts.io/aleksi/nut
    nut install http://gonuts.io/aleksi/nut/0.2.0
`

	cmdGet.Flag.StringVar(&getP, "p", "", "install prefix in workspace, uses hostname from URL if omitted")
	cmdGet.Flag.BoolVar(&getV, "v", false, vHelp)
}

// Parse argument, return URL to get nut from and install prefix.
func ParseArg(s string) (u *url.URL, prefix string) {
	var p []string
	var host string
	var ok bool

	// full URL - as is
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		goto parse
	}

	p = strings.Split(s, "/")
	if len(p) > 0 {
		prefix = p[0]
		host, ok = NutImportPrefixes[prefix]
	}
	if ok {
		// import path style
		p[0] = "http://" + host
		s = strings.Join(p, "/")
	} else {
		// short style
		prefix = "gonuts.io"
		host = NutImportPrefixes[prefix]
		s = fmt.Sprintf("http://%s/%s", host, s)
	}

parse:
	u, err := url.Parse(s)
	FatalIfErr(err)
	if prefix == "" {
		prefix = u.Host
		if strings.Contains(prefix, ":") {
			prefix, _, err = net.SplitHostPort(prefix)
			FatalIfErr(err)
		}
		if strings.HasPrefix(prefix, "www.") {
			prefix = prefix[4:]
		}
	}

	return
}

func get(url *url.URL) (b []byte, err error) {
	if getV {
		log.Printf("Getting %s ...", url)
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "nut getter")
	req.Header.Set("Accept", "application/zip")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	b, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return
	}

	if res.StatusCode/100 != 2 {
		err = fmt.Errorf("Status code %d", res.StatusCode)
		return
	}

	if getV {
		log.Printf("Status code %d", res.StatusCode)
	}

	return
}

func runGet(cmd *Command) {
	if !getV {
		getV = Config.V
	}

	args := cmd.Flag.Args()

	// zero arguments is a special case – install dependencies for package in current directory
	if len(args) == 0 {
		pack, err := build.ImportDir(".", 0)
		FatalIfErr(err)
		args = NutImports(pack.Imports)
		if getV && len(args) != 0 {
			log.Printf("%s depends on nuts: %s", pack.Name, strings.Join(args, ","))
		}
	}

	urlsToPaths := make(map[string]string, len(args))
	for len(args) != 0 {
		arg := args[0]
		args = args[1:]

		url, prefix := ParseArg(arg)

		// do not download twice
		_, present := urlsToPaths[url.String()]
		if present {
			continue
		}

		b, err := get(url)
		if err != nil {
			log.Print(err)

			var body map[string]interface{}
			err = json.Unmarshal(b, &body)
			if err != nil {
				log.Print(err)
			}
			m, ok := body["Message"]
			if ok {
				log.Fatalf("%s", m)
			} else {
				log.Fatalf("Response: %#q", body)
			}
		}

		nf := new(NutFile)
		_, err = nf.ReadFrom(bytes.NewReader(b))
		FatalIfErr(err)
		deps := NutImports(nf.Imports)
		if getV && len(deps) != 0 {
			log.Printf("%s depends on nuts: %s", nf.Name, strings.Join(deps, ", "))
		}
		args = append(args, deps...)

		p := getP
		if p == "" {
			p = prefix
		}
		fileName := WriteNut(b, p, getV)
		path := nf.ImportPath(p)
		UnpackNut(fileName, filepath.Join(SrcDir, path), true, getV)
		urlsToPaths[url.String()] = path
	}

	// install in lexical order (useful in integration tests)
	paths := make([]string, 0, len(urlsToPaths))
	for _, path := range urlsToPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		InstallPackage(path, getV)
	}
}
