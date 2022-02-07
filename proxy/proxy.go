package proxy

import (
	"bytes"
	"net"

	log "github.com/sirupsen/logrus"
)

type ProxyThread struct {
	ID           string
	TargetConn   net.Conn
	IncomingConn net.Conn
	Dead         bool
}

func NewProxyThread(ID string, conn net.Conn, targetConn net.Conn) *ProxyThread {
	p := &ProxyThread{IncomingConn: conn, TargetConn: targetConn, Dead: false, ID: ID}
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
		buf := bytes.NewBuffer(make([]byte, 100))

		n, err := p.IncomingConn.Read(buf.Bytes())
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error reading data to send to target")
			p.Close()
			return
		}
		buf.Truncate(n)

		_, err = p.TargetConn.Write(buf.Bytes())
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error writing data to the target")
			p.Close()
			return
		}
	}
}

func (p *ProxyThread) HandleFromTarget() {
	for !p.Dead {
		buf := bytes.NewBuffer(make([]byte, 100))
		n, err := p.TargetConn.Read(buf.Bytes())
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error reading data from the target to forward")
			p.Close()
			return
		}

		buf.Truncate(n)
		_, err = p.IncomingConn.Write(buf.Bytes())
		if err != nil {
			log.WithField("Id", p.ID).WithError(err).Error("Error writing data from target during forward")
			p.Close()
			return
		}
	}
}
