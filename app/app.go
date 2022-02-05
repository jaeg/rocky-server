package app

import (
	"context"
	"flag"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jaeg/rocky-server/proxy"
	log "github.com/sirupsen/logrus"
)

const AppName = "rocker-server"

var certFile = flag.String("cert-file", "", "location of cert file")
var keyFile = flag.String("key-file", "", "location of key file")

var tunnelPort = flag.String("tunnel-port", ":9998", "Port that is used for individual proxying requests")
var serverPort = flag.String("server-port", ":9999", "Port rocky clients connect to for management")
var proxyPort = flag.String("proxy-port", ":8099", "Port real clients connect to IE the exposed port")

type App struct {
	tunnels        map[string]net.Conn
	tunnelLock     *sync.Mutex
	tunnelListener net.Listener
	serverListener net.Listener
	proxyListener  net.Listener
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
	a.serverListener, err = net.Listen("tcp", *serverPort)
	if err != nil {
		log.WithError(err).Error("Error listening on server port")
		return
	}

	a.proxyListener, err = net.Listen("tcp", *proxyPort)
	if err != nil {
		log.WithError(err).Error("Error listening on proxy port")

		return
	}

	a.tunnelListener, err = net.Listen("tcp", *tunnelPort)
	if err != nil {
		log.WithError(err).Error("Error listening on communication port")
		return
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
					//New client with traffic that needs forwarded to it.
					go a.handleClient(conn)
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
	log.Debug("Handling new client")
	defer log.Info("handleClient thread died")

	for {
		//Accept incoming traffic to proxy.
		incomingRequestConn, err := a.proxyListener.Accept()
		if err != nil {
			log.WithError(err).Error("Error accepting proxy client")
			continue
		}

		log.Info("New connection to proxy")
		//Generate a new id and send it to the proxy client to signal it to create a new tunnel connection.
		generatedId := uuid.New().String()
		_, err = clientManagementConn.Write([]byte("New\n"))

		if err != nil {
			log.WithField("Id", generatedId).WithError(err).Error("Failed writing to management port")
			incomingRequestConn.Close()
			return
		}

		_, err = clientManagementConn.Write([]byte(generatedId + "\n"))
		if err != nil {
			log.WithField("Id", generatedId).WithError(err).Error("Failed writing to management port")
			incomingRequestConn.Close()
			return
		}

		log.WithField("Id", generatedId).Debug("Sent generated id to proxy client, waiting for response")

		// Wait for the response from the proxy client and confirm IDs.
		buf := make([]byte, len(generatedId))
		_, err = clientManagementConn.Read(buf)

		if err != nil {
			log.WithField("Id", generatedId).WithError(err).Error("Failed reading from management port")
			incomingRequestConn.Close()
			return
		}

		id := string(buf)
		log.WithField("Id", id).Debug("ID returned from the proxy client, checking for match.")
		if id != generatedId {
			log.WithFields(log.Fields{"ID": id, "Gen ID": generatedId}).Error("Proxy failed, mismatched IDs")
			incomingRequestConn.Close()
			continue
		}

		//Wait for the proxy client to open a tunnel between it and this server.
		log.WithField("Id", id).Debug("Waiting for connection from proxy client")
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
			log.WithField("Id", id).Error("Timed out waiting for connection from client")
			a.tunnelLock.Unlock()
			continue
		}
		a.tunnelLock.Unlock()

		//Create the thread to proxy the data through the tunnel.
		log.WithField("Id", id).Debug("Connection made with client, creating proxy thread.")
		proxy.NewProxyThread(incomingRequestConn, a.tunnels[id])
	}
}
