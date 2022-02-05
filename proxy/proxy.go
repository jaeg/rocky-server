package proxy

import (
	"net"

	log "github.com/sirupsen/logrus"
)

type ProxyThread struct {
	ID           string
	TargetConn   net.Conn
	IncomingConn net.Conn
	Dead         bool
}

func NewProxyThread(conn net.Conn, targetConn net.Conn) *ProxyThread {
	p := &ProxyThread{IncomingConn: conn, TargetConn: targetConn, Dead: false}
	go p.HandleFromTarget()
	go p.HandleToTarget()
	return p
}

func (p *ProxyThread) Close() {
	p.Dead = true
	p.TargetConn.Close()
	p.IncomingConn.Close()
}

func (p *ProxyThread) HandleToTarget() {
	for !p.Dead {
		buf := make([]byte, 1)
		_, err := p.IncomingConn.Read(buf)
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error reading data to send to target")
			p.Close()
			return
		}

		_, err = p.TargetConn.Write(buf)
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error writing data to the target")
			p.Close()
			return
		}
	}
}

func (p *ProxyThread) HandleFromTarget() {
	for !p.Dead {
		buf := make([]byte, 1)
		_, err := p.TargetConn.Read(buf)
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error reading data from the target to forward")
			p.Close()
			return
		}
		_, err = p.IncomingConn.Write(buf)
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error writing data from target during forward")
			p.Close()
			return
		}
	}
}
