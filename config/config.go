package config

import "github.com/spf13/viper"

type Config struct {
	VTEP VTEP `mapstructure:"vtep"`
}

type VTEP struct {
	VXLANs []VXLAN `mapstructure:"vxlans"`
	BGP    BGP     `mapstructure:"bgp"`
}

type VXLAN struct {
	VNI    int    `mapstructure:"vni"`
	Bridge Bridge `mapstructure:"bridge"`
}

type Bridge struct {
	Members []string `mapstructure:"members"`
}

type BGP struct {
	AS              uint32    `mapstructure:"as"`
	RouterID        string    `mapstructure:"router-id"`
	ListenPort      int       `mapstructure:"listen-port"`
	ListenAddresses []string  `mapstructure:"listen-addresses"`
	Neighbors       Neighbors `mapstructure:"neighbors"`
	API             API       `mapstructure:"api"`
}

type Neighbors struct {
	Connect []Connect `mapstructure:"connect"`
	Accept  []Accept  `mapstructure:"accept"`
}

type Connect struct {
	Address string `mapstructure:"address"`
	Port    uint16 `mapstructure:"port"`
}

type Accept struct {
	Prefix string `mapstructure:"prefix"`
}

type API struct {
	Listen string `mapstructure:"listen"`
}

func SetDefaults(v *viper.Viper) {
	v.SetDefault("vtep.bgp.listen-port", -1)
	v.SetDefault("vtep.bgp.listen-addresses", []string{"0.0.0.0", "::"})
	v.SetDefault("vtep.bgp.neighbors.connect.port", 179)
}
