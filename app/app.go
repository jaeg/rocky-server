package app

import (
	"context"
	"flag"
	"net"
	"os"
	"sync"
	"time"

	"github.com/google/logger"
	"github.com/google/uuid"
)

const AppName = "rocker-server"

var certFile = flag.String("cert-file", "", "location of cert file")
var keyFile = flag.String("key-file", "", "location of key file")
var logPath = flag.String("log-path", "./logs.txt", "Logs location")

var communicationPort = flag.String("communication-port", ":9998", "Port that is used for individual proxying requests")
var serverPort = flag.String("server-port", ":9999", "Port rocky clients connect to for management")
var proxyPort = flag.String("proxy-port", ":8099", "Port real clients connect to IE the exposed port")

type App struct {
	connections           map[string]net.Conn
	connectionLock        *sync.Mutex
	communicationListener net.Listener
	serverListener        net.Listener
	proxyListener         net.Listener
}

func (a *App) Init() {
	a.connections = make(map[string]net.Conn)
	a.connectionLock = &sync.Mutex{}
	flag.Parse()

	//Start the logger
	lf, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0660)
	if err != nil {
		logger.Fatalf("Failed to open log file: %v", err)
	}

	logger.Init(AppName, true, true, lf)

	logger.Infof("%s Starting", AppName)

	a.serverListener, err = net.Listen("tcp", *serverPort)
	if err != nil {
		logger.Errorf("Error listening on server port  %s", err.Error())
		return
	}

	a.proxyListener, err = net.Listen("tcp", *proxyPort)
	if err != nil {
		logger.Errorf("Error listening on proxy port  %s", err.Error())

		return
	}

	a.communicationListener, err = net.Listen("tcp", *communicationPort)
	if err != nil {
		logger.Errorf("Error listening on communication port  %s", err.Error())
		return
	}
}

func (a *App) Run(ctx context.Context) {
	defer logger.Close()
	go a.handleProxy(a.communicationListener)
	//Run the http server
	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("Killing thread")
			default:
				conn, err := a.serverListener.Accept()
				if err != nil {
					logger.Errorf("Error accepting incoming message on server port: %s", err.Error())
				} else {
					//New client with traffic that needs forwarded to it.
					go a.handleIncomingClients(conn)
				}
			}
		}
	}()

	// Handle shutdowns gracefully
	<-ctx.Done()

	logger.Info("Server shutdown")
}

func (a *App) handleProxy(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			logger.Errorf("Proxy thread failed:  %s", err.Error())
			return
		}
		buf := make([]byte, 36)
		conn.Read(buf)
		id := string(buf)

		a.connectionLock.Lock()
		a.connections[id] = conn
		a.connectionLock.Unlock()
	}
}

func handleToTarget(conn net.Conn, targetConn net.Conn) {
	for {
		buf := make([]byte, 1)
		_, err := conn.Read(buf)
		if err != nil {
			logger.Errorf("Error reading to target:  %s", err.Error())
			return
		}

		targetConn.Write(buf)
	}
}

func handleFromTarget(conn net.Conn, targetConn net.Conn) {
	for {
		buf := make([]byte, 1)
		_, err := targetConn.Read(buf)
		if err != nil {
			logger.Errorf("Error reading from target  %s", err.Error())

			return
		}
		conn.Write(buf)
	}
}

func (a *App) handleIncomingClients(targetConn net.Conn) {
	logger.Info("Handling new client")
	defer logger.Info("Incoming Thread Died")

	for {
		clientConn, err := a.proxyListener.Accept()
		if err != nil {
			logger.Errorf("Error accepting  %s", err.Error())
			return
		}

		logger.Info("New connection to proxy")
		generatedId := uuid.New().String()
		targetConn.Write([]byte("New\n"))
		targetConn.Write([]byte(generatedId + "\n"))

		logger.Infof("Send id %s", generatedId)
		buf := make([]byte, len(generatedId))
		targetConn.Read(buf)
		id := string(buf)
		logger.Infof("ID returned %s", id)
		if id != generatedId {
			logger.Error("Proxy failed, mismatched IDs")
			return
		}

		t := time.Now()
		for time.Since(t) < time.Second {

			a.connectionLock.Lock()
			if a.connections[id] != nil {
				a.connectionLock.Unlock()
				break
			}
			a.connectionLock.Unlock()
			time.Sleep(time.Millisecond)
		}

		a.connectionLock.Lock()
		if a.connections[id] == nil {
			logger.Error("Timed out waiting for connection from client")
			return
		}
		a.connectionLock.Unlock()

		go handleToTarget(clientConn, a.connections[id])
		go handleFromTarget(clientConn, a.connections[id])
	}
}
