package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/logdna/logdna-go/logger"
	"github.com/sisu-network/lib/log"
	"github.com/sodiumlabs/deyes/client"
	"github.com/sodiumlabs/deyes/config"
	"github.com/sodiumlabs/deyes/core"
	"github.com/sodiumlabs/deyes/core/oracle"
	"github.com/sodiumlabs/deyes/database"
	"github.com/sodiumlabs/deyes/network"
	"github.com/sodiumlabs/deyes/server"
)

func initializeDb(cfg *config.Deyes) database.Database {
	db := database.NewDb(cfg)
	err := db.Init()
	if err != nil {
		panic(err)
	}

	return db
}

func setupApiServer(cfg *config.Deyes, processor *core.Processor) {
	handler := rpc.NewServer()
	handler.RegisterName("deyes", server.NewApi(processor))

	log.Info("Running server at port", cfg.ServerPort)
	s := server.NewServer(handler, cfg.ServerPort)
	s.Run()
}

func initialize(cfg *config.Deyes) {
	db := initializeDb(cfg)

	sodiumClient := client.NewClient(cfg.SodiumServerUrl)
	go sodiumClient.TryDial()

	networkHttp := network.NewHttp()
	priceManager := oracle.NewTokenPriceManager(cfg.PriceProviders, cfg.Tokens, networkHttp)

	processor := core.NewProcessor(cfg, db, sodiumClient, priceManager)
	processor.Start()

	setupApiServer(cfg, processor)
}

func main() {
	configPath := os.Getenv("DEYES_CONFIG_PATH")
	cfg := config.Load(configPath)
	if len(cfg.LogDNA.Secret) > 0 {
		opts := logger.Options{
			App:           cfg.LogDNA.AppName,
			FlushInterval: cfg.LogDNA.FlushInterval.Duration,
			Hostname:      cfg.LogDNA.HostName,
			MaxBufferLen:  cfg.LogDNA.MaxBufferLen,
		}
		logDNA := log.NewDNALogger(cfg.LogDNA.Secret, opts, false)
		log.SetLogger(logDNA)
	}

	initialize(&cfg)

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	<-c
}
