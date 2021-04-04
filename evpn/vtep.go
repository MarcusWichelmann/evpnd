package evpn

import (
	"context"
	"github.com/google/go-cmp/cmp"
	"github.com/marcuswichelmann/evpnd/config"
	gobgpapi "github.com/osrg/gobgp/api"
	gobgp "github.com/osrg/gobgp/pkg/server"
	log "github.com/sirupsen/logrus"
)

type VTEP struct {
	bgpServer        *gobgp.BgpServer
	bgpServerStarted bool
	dataplane        *Dataplane
}

const PeerGroupName = "evpn-peers"

func NewVTEP() *VTEP {
	return &VTEP{
		bgpServer: nil,
		dataplane: NewDataplane(),
	}
}

func (vtep *VTEP) Configure(ctx context.Context, cfg config.VTEP) error {
	log.Debug("Configuring the VTEP...")

	if err := vtep.configureBGPServer(ctx, cfg); err != nil {
		return err
	}
	if err := vtep.configurePeerGroup(ctx, cfg); err != nil {
		return err
	}
	if err := vtep.configureDynamicNeighbors(ctx, cfg); err != nil {
		return err
	}
	if err := vtep.configurePeers(ctx, cfg); err != nil {
		return err
	}

	log.Info("VTEP configured.")

	return nil
}

func (vtep *VTEP) configureBGPServer(ctx context.Context, cfg config.VTEP) error {
	log.Debug("Configuring BGP server...")

	// Initialize BGP server
	if vtep.bgpServer == nil {
		if cfg.BGP.API.Listen != "" {
			vtep.bgpServer = gobgp.NewBgpServer(gobgp.GrpcListenAddress(cfg.BGP.API.Listen))
			log.Infof("BGP server is listening on %s.", cfg.BGP.API.Listen)
		} else {
			vtep.bgpServer = gobgp.NewBgpServer()
		}

		go func() {
			vtep.bgpServer.Serve()
			log.Error("BGP server routine has ended.")
		}()
	}

	var global = gobgpapi.Global{
		As:              cfg.BGP.AS,
		RouterId:        cfg.BGP.RouterID,
		ListenPort:      int32(cfg.BGP.ListenPort),
		ListenAddresses: cfg.BGP.ListenAddresses,
	}

	// Was ist already running?
	if vtep.bgpServerStarted {
		// Retrieve current server configuration
		resp, err := vtep.bgpServer.GetBgp(ctx, &gobgpapi.GetBgpRequest{})
		if err != nil {
			return err
		}

		// Has the configuration changed?
		if resp.Global.As == global.As &&
			resp.Global.RouterId == global.RouterId &&
			resp.Global.ListenPort == global.ListenPort &&
			cmp.Equal(resp.Global.ListenAddresses, global.ListenAddresses) {
			log.Info("BGP server configuration has not changed.")
			return nil
		}

		// Stop the server first
		log.Debug("Stopping BGP server so it can be reconfigured...")
		if err := vtep.bgpServer.StopBgp(ctx, &gobgpapi.StopBgpRequest{}); err != nil {
			return err
		}
	}

	log.Debug("Starting BGP server...")
	if err := vtep.bgpServer.StartBgp(ctx, &gobgpapi.StartBgpRequest{
		Global: &global,
	}); err != nil {
		return err
	}

	vtep.bgpServerStarted = true

	log.WithFields(log.Fields{
		"AS":       cfg.BGP.AS,
		"RouterID": cfg.BGP.RouterID,
		"Port":     cfg.BGP.ListenPort,
	}).Info("BGP server configured.")

	return nil
}

func (vtep *VTEP) configurePeerGroup(ctx context.Context, cfg config.VTEP) error {
	log.Debug("Configuring peer group...")

	var peerGroup = gobgpapi.PeerGroup{
		Conf: &gobgpapi.PeerGroupConf{
			PeerGroupName: PeerGroupName,
			Description:   "EVPN Peers",
			PeerAs:        cfg.BGP.AS, // For now, only peers on the same AS are supported
		},
		AfiSafis: []*gobgpapi.AfiSafi{
			{
				Config: &gobgpapi.AfiSafiConfig{
					Enabled: true,
					Family: &gobgpapi.Family{
						Afi:  gobgpapi.Family_AFI_L2VPN,
						Safi: gobgpapi.Family_SAFI_EVPN,
					},
				},
			},
		},
	}

	// Check if the peer group already exists
	var existingPeerGroup *gobgpapi.PeerGroup = nil
	err := vtep.bgpServer.ListPeerGroup(ctx, &gobgpapi.ListPeerGroupRequest{
		PeerGroupName: PeerGroupName,
	}, func(pg *gobgpapi.PeerGroup) {
		// This callback gets called synchronously, so no synchronization required.
		existingPeerGroup = pg
	})
	if err != nil {
		return err
	}

	if existingPeerGroup != nil {
		// Has the peer group changed?
		if existingPeerGroup.Conf.PeerGroupName == peerGroup.Conf.PeerGroupName &&
			existingPeerGroup.Conf.Description == peerGroup.Conf.Description &&
			existingPeerGroup.Conf.PeerAs == peerGroup.Conf.PeerAs &&
			len(existingPeerGroup.AfiSafis) == len(peerGroup.AfiSafis) &&
			existingPeerGroup.AfiSafis[0].Config.Enabled == peerGroup.AfiSafis[0].Config.Enabled &&
			existingPeerGroup.AfiSafis[0].Config.Family.Afi == peerGroup.AfiSafis[0].Config.Family.Afi &&
			existingPeerGroup.AfiSafis[0].Config.Family.Safi == peerGroup.AfiSafis[0].Config.Family.Safi {
			log.Info("Peer group has not changed.")
			return nil
		}

		if _, err := vtep.bgpServer.UpdatePeerGroup(ctx, &gobgpapi.UpdatePeerGroupRequest{
			PeerGroup: &peerGroup,
		}); err != nil {
			return err
		}
		log.Infof("Peer group %s updated.", PeerGroupName)
	} else {
		if err := vtep.bgpServer.AddPeerGroup(ctx, &gobgpapi.AddPeerGroupRequest{
			PeerGroup: &peerGroup,
		}); err != nil {
			return err
		}
		log.Infof("Peer group %s added.", PeerGroupName)
	}

	return nil
}

func (vtep *VTEP) configureDynamicNeighbors(ctx context.Context, cfg config.VTEP) error {
	log.Debug("Configuring dynamic neighbors...")

	var toBeDeleted []*gobgpapi.DynamicNeighbor
	var toBeCreated = make([]*gobgpapi.DynamicNeighbor, len(cfg.BGP.Neighbors.Accept))
	for i, accept := range cfg.BGP.Neighbors.Accept {
		toBeCreated[i] = &gobgpapi.DynamicNeighbor{
			Prefix:    accept.Prefix,
			PeerGroup: PeerGroupName,
		}
	}

	// Check for which dynamic neighbor entries already exist
	err := vtep.bgpServer.ListDynamicNeighbor(ctx, &gobgpapi.ListDynamicNeighborRequest{
		PeerGroup: PeerGroupName,
	}, func(dn *gobgpapi.DynamicNeighbor) {
		// Was it in the to-be-created slice?
		var toBeCreatedIndex = -1
		for i, tbc := range toBeCreated {
			if tbc.Prefix == dn.Prefix && tbc.PeerGroup == dn.PeerGroup {
				toBeCreatedIndex = i
				break
			}
		}

		if toBeCreatedIndex == -1 {
			// This entry is no longer needed and should be deleted
			toBeDeleted = append(toBeDeleted, dn)
		} else {
			// No need to create this entry because it already exists
			toBeCreated[toBeCreatedIndex] = toBeCreated[len(toBeCreated)-1]
			toBeCreated = toBeCreated[:len(toBeCreated)-1]
		}
	})
	if err != nil {
		return err
	}

	// Delete no longer needed entries
	for _, tbd := range toBeDeleted {
		if err := vtep.bgpServer.DeleteDynamicNeighbor(ctx, &gobgpapi.DeleteDynamicNeighborRequest{
			Prefix:    tbd.Prefix,
			PeerGroup: tbd.PeerGroup,
		}); err != nil {
			return err
		}
		log.Infof("Dynamic neighbor entry for %s deleted.", tbd.Prefix)
	}

	// Create new entries
	for _, tbc := range toBeCreated {
		if err := vtep.bgpServer.AddDynamicNeighbor(ctx, &gobgpapi.AddDynamicNeighborRequest{
			DynamicNeighbor: tbc,
		}); err != nil {
			return err
		}
		log.Infof("Dynamic neighbor entry for %s created.", tbc.Prefix)
	}

	if len(toBeCreated) == 0 && len(toBeDeleted) == 0 {
		log.Info("Dynamic neighbors have not changed.")
	}

	return nil
}

func (vtep *VTEP) configurePeers(ctx context.Context, cfg config.VTEP) error {
	log.Debug("Configuring peers...")

	var toBeDeleted []*gobgpapi.Peer
	var toBeCreated = make([]*gobgpapi.Peer, len(cfg.BGP.Neighbors.Connect))
	for i, connect := range cfg.BGP.Neighbors.Connect {
		toBeCreated[i] = &gobgpapi.Peer{
			Conf: &gobgpapi.PeerConf{
				NeighborAddress: connect.Address,
				PeerAs:          cfg.BGP.AS, // For now, only peers on the same AS are supported
			},
			Transport: &gobgpapi.Transport{
				RemotePort: uint32(connect.Port),
			},
			AfiSafis: []*gobgpapi.AfiSafi{
				{
					Config: &gobgpapi.AfiSafiConfig{
						Enabled: true,
						Family: &gobgpapi.Family{
							Afi:  gobgpapi.Family_AFI_L2VPN,
							Safi: gobgpapi.Family_SAFI_EVPN,
						},
					},
				},
			},
		}
	}

	// Check for which peer entries already exist
	err := vtep.bgpServer.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		if peer.Conf.PeerGroup != PeerGroupName {
			return
		}

		// Was it in the to-be-created slice?
		var toBeCreatedIndex = -1
		for i, tbc := range toBeCreated {
			if tbc.Conf.NeighborAddress == peer.Conf.NeighborAddress &&
				tbc.Conf.PeerAs == peer.Conf.PeerAs &&
				tbc.Transport.RemotePort == peer.Transport.RemotePort &&
				len(tbc.AfiSafis) == len(peer.AfiSafis) &&
				tbc.AfiSafis[0].Config.Enabled == peer.AfiSafis[0].Config.Enabled &&
				tbc.AfiSafis[0].Config.Family.Afi == peer.AfiSafis[0].Config.Family.Afi &&
				tbc.AfiSafis[0].Config.Family.Safi == peer.AfiSafis[0].Config.Family.Safi {
				toBeCreatedIndex = i
				break
			}
		}

		if toBeCreatedIndex == -1 {
			// This entry is no longer needed and should be deleted
			toBeDeleted = append(toBeDeleted, peer)
		} else {
			// No need to create this entry because it already exists
			toBeCreated[toBeCreatedIndex] = toBeCreated[len(toBeCreated)-1]
			toBeCreated = toBeCreated[:len(toBeCreated)-1]
		}
	})
	if err != nil {
		return err
	}

	// Delete no longer needed entries
	for _, tbd := range toBeDeleted {
		if err := vtep.bgpServer.DeletePeer(ctx, &gobgpapi.DeletePeerRequest{
			Address: tbd.Conf.NeighborAddress,
		}); err != nil {
			return err
		}
		log.Infof("Peer %s deleted.", tbd.Conf.NeighborAddress)
	}

	// Create new entries
	for _, tbc := range toBeCreated {
		if err := vtep.bgpServer.AddPeer(ctx, &gobgpapi.AddPeerRequest{
			Peer: tbc,
		}); err != nil {
			return err
		}
		log.Infof("Peer %s created.", tbc.Conf.NeighborAddress)
	}

	if len(toBeCreated) == 0 && len(toBeDeleted) == 0 {
		log.Info("Peers have not changed.")
	}

	return nil
}
