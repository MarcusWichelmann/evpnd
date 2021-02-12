package main

import (
	"github.com/jessevdk/go-flags"
	"github.com/marcuswichelmann/evpnd/config"
	"github.com/marcuswichelmann/evpnd/evpn"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
)

// CLI options
type Options struct {
	ConfigFile string `short:"f" long:"config-file" description:"The config file to load, can be a .toml, .yaml or .json"`
	LogLevel   string `short:"l" long:"log-level" description:"The log level" choice:"debug" choice:"info" choice:"warn" default:"info"`
	LogJson    bool   `short:"j" long:"log-json" description:"Write logs as json"`
}

var options Options
var flagsParser = flags.NewParser(&options, flags.Default)

func main() {
	// Parse CLI options
	if _, err := flagsParser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok {
			if flagsErr.Type == flags.ErrHelp {
				os.Exit(0)
			} else {
				os.Exit(1)
			}
		} else {
			log.Fatal(err)
		}
	}

	// Set log level
	switch options.LogLevel {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	default:
		log.Fatal("Unknown log level")
	}

	// Configure logging
	if options.LogJson {
		log.SetFormatter(&log.JSONFormatter{})
	}

	// Specify configuration file location
	if options.ConfigFile != "" {
		viper.SetConfigFile(options.ConfigFile)
	} else {
		viper.SetConfigName("config")
		viper.AddConfigPath("/etc/goevpn/")
		viper.AddConfigPath(".")
	}

	// Read configuration file
	log.Debug("Parsing configuration...")
	if err := viper.ReadInConfig(); err != nil {
		log.WithField("file", viper.ConfigFileUsed()).WithError(err).Fatal("Error reading config file. ")
	}

	// Unmarshal configuration
	var config config.Config
	if err := viper.Unmarshal(&config); err != nil {
		log.WithField("file", viper.ConfigFileUsed()).WithError(err).Fatal("Error unmarshaling config file. ")
	}
	log.WithField("file", viper.ConfigFileUsed()).Debug("Configuration parsed.")

	// Configure the VTEP
	log.Debug("Configuring the VTEP...")
	vtep := evpn.NewVTEP(&config.VTEP)
	if err := vtep.Configure(); err != nil {
		log.WithError(err).Error("Configuring VTEP failed.")
	}
}
