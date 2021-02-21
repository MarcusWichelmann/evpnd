package main

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"github.com/jessevdk/go-flags"
	"github.com/marcuswichelmann/evpnd/config"
	"github.com/marcuswichelmann/evpnd/evpn"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"os/signal"
	"syscall"
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
	// Context for the application lifetime
	var ctx = context.Background()
	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

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

	// Configure logging
	logLevel, err := log.ParseLevel(options.LogLevel)
	if err != nil {
		log.WithError(err).Fatal("Unknown log level")
	}
	log.SetLevel(logLevel)
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
	config.SetDefaults(viper.GetViper())
	if err := viper.ReadInConfig(); err != nil {
		log.WithField("File", viper.ConfigFileUsed()).WithError(err).Fatal("Error reading config file. ")
	}

	// Initialize the VTEP
	var vtep = evpn.NewVTEP()

	// Create a channel for reconfiguring the daemon
	var reconfigure = make(chan struct{}, 1)
	reconfigure <- struct{}{} // Deamon should be initially configured

	// Receive SIGTERM signal
	var terminate = make(chan os.Signal)
	signal.Notify(terminate, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(terminate)

	// Watch for configuration changes
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info("Configuration file changed. Reconfiguring daemon...")

		reconfigure <- struct{}{}
	})

	// Handle events
EventLoop:
	for {
		select {
		case <-reconfigure:
			// Unmarshal configuration
			var cfg config.Config
			if err := viper.Unmarshal(&cfg); err != nil {
				log.WithField("File", viper.ConfigFileUsed()).WithError(err).Fatal("Error unmarshaling config file. ")
			}
			log.WithField("File", viper.ConfigFileUsed()).Debug("Configuration parsed.")

			// Reconfigure the VTEP
			if err := vtep.Configure(ctx, cfg.VTEP); err != nil {
				log.WithError(err).Fatal("Configuring the VTEP failed.")
			}

		case <-terminate:
			log.Info("Received terminate signal. Exiting...")

			break EventLoop
		}
	}

	// TODO: Make sure everything is stopped
}
