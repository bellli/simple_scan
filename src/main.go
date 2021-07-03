package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	s "strings"
	"sync"
	"time"
	"github.com/tkanos/gonfig"
	"golang.org/x/net/proxy"
)

// Configuration ...
type Configuration struct {
	Ports               []string
	Networks            []string
	ConcurrentlyHost    int
	ConcurrentlyPerHost int
	Socks               []string
	ConnectionTimeout   int64
	ShowErrors          bool
}

type intRange struct {
	begin int
	end   int
}

type target struct {
	ip   string
	port string
}

type host struct {
	ip        string
	openPorts []string
}

type state struct {
	tryCnt              int
	hostCnt             int
	maxtryCnt           int
	portCnt             int
	proxy               string
	proto               string
	connectionTimeout   int64
	ports               []string
	concurrentlyHost    int
	concurrentlyPerHost int
	showErrors          bool
}

type conStatus struct {
	target
	err error
}

type outputResult map[string]*host

var app state

func sendToChan(data []string, in chan string) {
	for _, addr := range data {
		in <- addr
	}
}

func main() {

	userNetwork := flag.String("net", "192.168.1.0/24", "net to scan; example net=192.168.1.0/24")
	ports := flag.String("p", "443,80,22", "set port example p=1-1024 or p=443,22,8080")
	concurrentlyPerHost := flag.Int("cph", 10, "num of concurrently reqs per host;  default: cph=10")
	concurrentlyHost := flag.Int("ch", 10, "concurrently host default: ch=10")
	connectionTimeout := flag.Int64("t", 5, "connection Timeout per sec default: t=5")
	output := flag.String("o", "result.txt", "output filename default: o=result.txt")
	socks := flag.String("s", "127.0.0.1:1080", "set socks example s=127.0.0.1:1080")
	pathToConf := flag.String("c", "nil", "path to config example c=config.json")
	flag.Parse()

	if *pathToConf != "nil" {
		configuration := Configuration{}
		err := gonfig.GetConf(*pathToConf, &configuration)
		if err != nil {
			panic(err)
		}

		for _, port := range configuration.Ports {
			ports := handlePort(port)
			app.ports = append(app.ports, ports...)
		}

		app.showErrors = false
		app.concurrentlyPerHost = *concurrentlyPerHost
		app.concurrentlyHost = *concurrentlyHost
		app.proto = "tcp"
		app.proxy = *socks
		app.connectionTimeout = *connectionTimeout
		fmt.Println("=========SETTINGS=========")
		fmt.Println("networks to scan ", configuration.Networks)
		fmt.Println("ports to check ", app.ports)
		fmt.Println("active socks ", app.proxy)
		fmt.Println("connection timeout set to ", app.connectionTimeout)
		fmt.Println("==========================")
		for _, remoteNet := range configuration.Networks {
			network, err := getNetworkHostList(remoteNet)
			if err != nil {
				fmt.Println("error while processing network " + remoteNet)
				continue
			}
			filename := s.ReplaceAll(remoteNet, "/", "_") + ".log"
			app.hostCnt = len(network)
			app.portCnt = len(app.ports)
			app.maxtryCnt = app.portCnt * app.hostCnt
			app.proxy = configuration.Socks[0]

			// TODO CHANNEL SIZE ???
			targets := make(chan string, 100)
			results := make(chan conStatus, 300)

			successChan := make(chan *host, 100)

			go sendToChan(network, targets)
			for i := 1; i <= app.concurrentlyHost; i++ {
				go hostWorker(targets, results)
			}
			go resultCollector(results, successChan)
			saveData(filename, successChan)
			time.Sleep(2 * time.Second)
			fmt.Println("network " + remoteNet + " is done")
		}
		fmt.Println("finish")
		os.Exit(0)
	}

}

func resultCollector(in chan conStatus, out chan *host) {
	resp := 0
	res := make(outputResult)
	for r := range in {
		resp++
		if r.err == nil {
			fmt.Println(r.ip, r.port, " found open")
			if _, ok := res[r.ip]; ok {
				res[r.ip].openPorts = append(res[r.ip].openPorts, r.port)
			} else {
				res[r.ip] = &host{r.ip, []string{r.port}}
			}

		} else if app.showErrors {
			fmt.Println(r.err)
		}

		if resp >= app.maxtryCnt {

			for _, v := range res {

				out <- v
			}

			time.Sleep(4 * time.Second)
			close(in)
			close(out)
		}
	}
}

func hostWorker(in chan string, out chan conStatus) {

	var wg sync.WaitGroup
	cnt := make(chan int, app.concurrentlyPerHost)
	for {

		select {
		case addr := <-in:
			for i, p := range app.ports {
				cnt <- i
				wg.Add(1)
				go tryConn(target{addr, p}, out, cnt, &wg)

			}
			wg.Wait()

		}

	}

}

func tryConn(t target, out chan conStatus, cnt chan int, wg *sync.WaitGroup) {

	c := context.Background()
	c, cancel := context.WithTimeout(c, time.Duration(app.connectionTimeout)*time.Second)
	defer func() {
		wg.Done()
		<-cnt
		cancel()

	}()
	data := make(chan conStatus, 1)

	go func() {
		dialer, err := proxy.SOCKS5("tcp", app.proxy, nil, proxy.Direct)
		conn, err := dialer.Dial("tcp", t.ip+":"+t.port)
		if err != nil {

			data <- conStatus{t, err}
			return
		}
		defer conn.Close()
		data <- conStatus{t, err}

	}()
	select {
	case <-c.Done():

		out <- conStatus{t, errors.New("Timeout while request to " + t.ip + ":" + t.port)}

	case cdata := <-data:

		out <- cdata

	}
}

func saveData(filename string, results chan *host) {
	for r := range results {

		str := r.ip + ", "
		for _, v := range r.openPorts {
			str += v + ";"
		}
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal(err)
		}

		if _, err := f.Write([]byte(str + "\n")); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}

}

func getNetworkHostList(cidrNetwork string) ([]string, error) {
	ip, network, err := net.ParseCIDR(cidrNetwork)
	if err != nil {
		log.Fatal(err)
	}
	var hosts []string
	for ip := ip.Mask(network.Mask); network.Contains(ip); inc(ip) {

		hosts = append(hosts, ip.String())

	}
	switch {
	case len(hosts) < 2:
		return hosts, nil
	default:
		return hosts[1 : len(hosts)-1], nil
	}
}

func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func checkError(message string, err error) {
	if err != nil {
		log.Fatal(message, err)
	}
}

func parsePort(value string, sep string) []string {
	if s.Contains(value, sep) {
		args := s.Split(value, sep)
		return args
	}
	return []string{value}
}

func handlePort(v string) []string {

	ports := []string{}
	if s.Contains(v, ",") {
		args := parsePort(v, ",")
		for _, el := range args {
			ports = append(ports, el)
		}
	} else if s.Contains(v, "-") {

		el := parsePort(v, "-")
		min, _ := strconv.Atoi(el[0])
		max, _ := strconv.Atoi(el[1])
		for i := min; i <= max; i++ {
			ports = append(ports, strconv.Itoa(i))
		}

	} else {
		ports = append(ports, v)
	}
	return ports

}
