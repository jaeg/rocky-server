package app

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"io/ioutil"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jaeg/rocky-server/proxy"
	log "github.com/sirupsen/logrus"

	"github.com/jpillora/ipfilter"
)

const AppName = "rocky-server"

var proxyCertFile = flag.String("proxy-cert", "", "location of cert file")
var proxyKeyFile = flag.String("proxy-key", "", "location of key file")

var communicationCertFilePath = flag.String("communication-cert", "certs/server.pem", "location of cert file")
var communicationKeyFilePath = flag.String("communication-key", "certs/server.key", "location of key file")
var communicationCAFilePath = flag.String("communication-ca", "certs/ca.crt", "location of ca file")

var tunnelPort = flag.String("tunnel-port", ":9998", "Port that is used for individual proxying requests")
var serverPort = flag.String("server-port", ":9999", "Port rocky clients connect to for management")
var proxyPort = flag.String("proxy-port", ":8099", "Port real clients connect to IE the exposed port")

var allowedIPs = flag.String("allowed-ips", "", "List of ips to allow.  Blank to allow all")
var blockedIPs = flag.String("blocked-ips", "", "List of ips to block.")

var blockedCountries = flag.String("blocked-countries", "", "List of countries to block.")

type App struct {
	tunnels        map[string]net.Conn
	tunnelLock     *sync.Mutex
	tunnelListener net.Listener
	serverListener net.Listener
	proxyListener  net.Listener
	ipFilter       *ipfilter.IPFilter
}

func (a *App) Init() {
	a.tunnels = make(map[string]net.Conn)
	a.tunnelLock = &sync.Mutex{}
	flag.Parse()

	//Start the logger
	log.SetLevel(log.DebugLevel)

	log.WithFields(log.Fields{
		"Name": AppName,
	}).Info("Started")

	var err error

	//Setup filter
	a.ipFilter = ipfilter.New(ipfilter.Options{BlockByDefault: *allowedIPs != ""})

	if *allowedIPs != "" {
		ips := strings.Split(*allowedIPs, ",")
		for _, v := range ips {
			log.WithField("IP", v).Info("Allowing ip")
			a.ipFilter.AllowIP(v)
		}
	}

	if *blockedIPs != "" {
		ips := strings.Split(*blockedIPs, ",")
		for _, v := range ips {
			log.WithField("IP", v).Info("Blocking ip")
			a.ipFilter.BlockIP(v)
		}
	}

	if *blockedCountries != "" {
		countries := strings.Split(*blockedCountries, ",")
		for _, v := range countries {
			log.WithField("Country", v).Info("Blocking country")

			a.ipFilter.BlockCountry(v)
		}
	}

	if *proxyCertFile != "" {
		log.Info("Start proxy listener with cert %s", *proxyCertFile)
		cert, err := tls.LoadX509KeyPair(*proxyCertFile, *proxyKeyFile)
		if err != nil {
			log.WithError(err).Error("proxy cert/key failed to load")
			return
		}
		config := tls.Config{Certificates: []tls.Certificate{cert}}

		config.Rand = rand.Reader
		a.proxyListener, err = tls.Listen("tcp", *proxyPort, &config)

		if err != nil {
			log.WithError(err).Error("tls proxy listener failed")
			return
		}

	} else {
		a.proxyListener, err = net.Listen("tcp", *proxyPort)
		if err != nil {
			log.WithError(err).Error("Error listening on proxy port")

			return
		}
	}

	if *communicationCertFilePath != "" {
		log.Info("Start proxy listener with cert %s", *communicationCertFilePath)
		cert, err := tls.LoadX509KeyPair(*communicationCertFilePath, *communicationKeyFilePath)
		if err != nil {
			log.WithError(err).Error("failed loading tunnel cert/key")
			return
		}

		caCert, err := ioutil.ReadFile(*communicationCAFilePath)
		if err != nil {
			log.Fatal(err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)

		config := tls.Config{Certificates: []tls.Certificate{cert},
			ClientCAs:  caCertPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
		}
		config.Rand = rand.Reader
		a.tunnelListener, err = tls.Listen("tcp", *tunnelPort, &config)
		if err != nil {
			log.WithError(err).Error("tls tunnel listener failed")
			return
		}

		a.serverListener, err = tls.Listen("tcp", *serverPort, &config)
		if err != nil {
			log.WithError(err).Error("tls server listener failed")
			return
		}

	} else {
		a.tunnelListener, err = net.Listen("tcp", *tunnelPort)
		if err != nil {
			log.WithError(err).Error("Error listening on communication port")
			return
		}

		a.serverListener, err = net.Listen("tcp", *serverPort)
		if err != nil {
			log.WithError(err).Error("Error listening on server port")
			return
		}
	}
}

func (a *App) Run(ctx context.Context) {
	go a.handleTunnelListener(a.tunnelListener)
	//Run the http server
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Info("Killing main thread")
			default:
				conn, err := a.serverListener.Accept()
				if err != nil {
					log.WithError(err).Error("Error accepting incoming message on server port")
				} else {
					ip := strings.Split(conn.RemoteAddr().String(), ":")
					if len(ip) > 0 {
						log.WithField("IP", ip[0]).Debug("Incoming client connection")
						if a.ipFilter.Allowed(ip[0]) {
							//New client with traffic that needs forwarded to it.
							go a.handleClient(conn)
						} else {
							log.WithField("IP", ip[0]).Error("IP blocked on client port")
							conn.Close()
						}

					} else {
						log.WithField("IP", ip[0]).Error("Invalid ip on client port")
						conn.Close()
					}
				}
			}
		}
	}()

	// Handle shutdowns gracefully
	<-ctx.Done()

	log.Info("Server shutdown")
}

// Checks for new tunnel connections to proxy data through and adds them to the connections map.
func (a *App) handleTunnelListener(tunnelListener net.Listener) {
	for {
		//Accept connections on the tunnel listener port.
		conn, err := tunnelListener.Accept()
		if err != nil {
			log.WithError(err).Error("Proxy thread failed")
			return
		}

		//Verify conn's origin
		ip := strings.Split(conn.RemoteAddr().String(), ":")
		if len(ip) > 0 {
			if a.ipFilter.Blocked(ip[0]) {
				log.WithField("IP", ip[0]).Error("Blocked ip coming into tunnel port")
				conn.Close()
				continue
			}
		} else {
			log.WithField("IP", ip[0]).Error("Invalid ip coming into tunnel port")
			conn.Close()
			continue
		}

		// First message in the tunnel is an ID.
		buf := make([]byte, 36)
		_, err = conn.Read(buf)
		if err != nil {
			log.WithError(err).Error("Failed reading from tunnel port")
			conn.Close()
			continue
		}
		id := string(buf)

		//Add ID to tunnel map.
		a.tunnelLock.Lock()
		a.tunnels[id] = conn
		a.tunnelLock.Unlock()
	}
}

//Handles listening for incoming proxy clients that want to expose their targets.
func (a *App) handleClient(clientManagementConn net.Conn) {
	myLogger := log.WithField("IP", clientManagementConn.RemoteAddr().String())
	myLogger.Debug("Handling new client")
	defer myLogger.Info("handleClient thread died")

	for {
		//Accept incoming traffic to proxy.
		incomingRequestConn, err := a.proxyListener.Accept()

		if err != nil {
			myLogger.WithError(err).Error("Error accepting proxy client")
			continue
		}

		myLogger.Info("New connection to proxy")
		//Generate a new id and send it to the proxy client to signal it to create a new tunnel connection.
		generatedId := uuid.New().String()
		_, err = clientManagementConn.Write([]byte("New\n"))

		if err != nil {
			myLogger.WithField("Id", generatedId).WithError(err).Error("Failed writing to management port")
			incomingRequestConn.Close()
			return
		}

		_, err = clientManagementConn.Write([]byte(generatedId + "\n"))
		if err != nil {
			myLogger.WithField("Id", generatedId).WithError(err).Error("Failed writing to management port")
			incomingRequestConn.Close()
			return
		}

		myLogger.WithField("Id", generatedId).Debug("Sent generated id to proxy client, waiting for response")

		// Wait for the response from the proxy client and confirm IDs.
		buf := make([]byte, len(generatedId))
		_, err = clientManagementConn.Read(buf)

		if err != nil {
			myLogger.WithField("Id", generatedId).WithError(err).Error("Failed reading from management port")
			incomingRequestConn.Close()
			return
		}

		id := string(buf)
		myLogger.WithField("Id", id).Debug("ID returned from the proxy client, checking for match.")
		if id != generatedId {
			myLogger.WithFields(log.Fields{"ID": id, "Gen ID": generatedId}).Error("Proxy failed, mismatched IDs")
			incomingRequestConn.Close()
			continue
		}

		//Wait for the proxy client to open a tunnel between it and this server.
		myLogger.WithField("Id", id).Debug("Waiting for connection from proxy client")
		t := time.Now()
		for time.Since(t) < time.Second {
			a.tunnelLock.Lock()
			if a.tunnels[id] != nil {
				a.tunnelLock.Unlock()
				break
			}
			a.tunnelLock.Unlock()
			time.Sleep(time.Nanosecond)
		}

		//Check to see if we got a connection within the timeout.
		a.tunnelLock.Lock()
		if a.tunnels[id] == nil {
			myLogger.WithField("Id", id).Error("Timed out waiting for connection from client")
			a.tunnelLock.Unlock()
			continue
		}
		a.tunnelLock.Unlock()

		//Create the thread to proxy the data through the tunnel.
		myLogger.WithField("Id", id).Debug("Connection made with client, creating proxy thread.")
		proxy.NewProxyThread(id, incomingRequestConn, a.tunnels[id])
	}
}
