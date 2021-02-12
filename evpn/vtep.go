package evpn

import (
	"github.com/marcuswichelmann/evpnd/config"
	gobgp "github.com/osrg/gobgp/pkg/server"
)

type VTEP struct {
	config    *config.VTEP
	bgpServer *gobgp.BgpServer
	dataplane *Dataplane
}

func NewVTEP(config *config.VTEP) *VTEP {
	return &VTEP{
		config:    config,
		dataplane: NewDataplane(),
	}
}

func (vtep *VTEP) Configure() error {
	gobgp.NewBgpServer()
	return nil
}
