package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

var version = "1.0.5"

type config struct {
	Server         string `json:"server,omitempty"`
	Port           int    `json:"port,omitempty"`
	Key            string `json:"key"`
	Interface      string `json:"interface,omitempty"`
	Listen         string `json:"listen,omitempty"`
	PublicIP       string `json:"public_ip,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ip":
		if err := runIP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "icmptunnel ip:", err)
			os.Exit(1)
		}
		return
	case "version", "--version":
		fmt.Println("icmptunnel", version)
		return
	case "keygen":
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		fmt.Println(hex.EncodeToString(b))
		return
	case "router", "server":
	default:
		usage()
		os.Exit(2)
	}
	f := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	path := f.String("config", "", "configuration JSON path")
	_ = f.Parse(os.Args[2:])
	if *path == "" || f.NArg() != 0 {
		f.Usage()
		os.Exit(2)
	}
	b, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	var cfg config
	if err = json.Unmarshal(b, &cfg); err != nil {
		log.Fatal(err)
	}
	if cfg.Port == 0 {
		cfg.Port = 39070
	}
	if cfg.Interface == "" {
		cfg.Interface = "icmptun0"
	}
	if cfg.Listen == "" {
		cfg.Listen = ":39070"
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 10
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; os.Exit(0) }() // kernel closes sockets and nonpersistent TUN
	if os.Args[1] == "router" {
		err = runRouter(cfg)
	} else {
		err = runServer(cfg)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: icmptunnel router|server --config FILE | keygen | version | ip COMMAND")
}

func ipv4(s string) (net.IP, error) {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() == nil {
		return nil, errors.New("expected literal IPv4 address")
	}
	return ip.To4(), nil
}
