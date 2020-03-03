/*
Copyright © 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Entry point to the insights results aggregator service.
//
// The service contains consumer (usually Kafka consumer) that consume
// messages from given source, processs those messages, and stores them
// in configured data store. It also starts REST API servers with
// endpoints that expose several types of information: list of organizations,
// list of clusters for given organization, and cluster health.
package main

import (
	"context"
	"github.com/RedHatInsights/insights-results-aggregator/rules"
	"log"
	"os"
	"time"

	"github.com/spf13/viper"

	"github.com/RedHatInsights/insights-results-aggregator/consumer"
	"github.com/RedHatInsights/insights-results-aggregator/server"
	"github.com/RedHatInsights/insights-results-aggregator/storage"
)

const (
	// ExitStatusOK means that the tool finished with success
	ExitStatusOK = iota
	// ExitStatusConsumerError is returned in case of any consumer-related error
	ExitStatusConsumerError
	// ExitStatusServerError is returned in case of any REST API server-related error
	ExitStatusServerError
)

var (
	serverInstance   *server.HTTPServer
	consumerInstance consumer.Consumer
	updaterInstance  *rules.Updater
)

func startStorageConnection() (*storage.DBStorage, error) {
	storageCfg := loadStorageConfiguration()
	storage, err := storage.New(storageCfg)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return storage, nil
}

// closeStorage closes specified DBStorage with proper error checking
// whether the close operation was successful or not.
func closeStorage(storage *storage.DBStorage) {
	err := storage.Close()
	if err != nil {
		log.Println("Error during closing storage connection", err)
	}
}

// closeConsumer closes specified consumer instance with proper error checking
// whether the close operation was successful or not.
func closeConsumer(consumerInstance consumer.Consumer) {
	err := consumerInstance.Close()
	if err != nil {
		log.Println("Error during closing consumer", err)
	}
}

func startConsumer() {
	storage, err := startStorageConnection()
	if err != nil {
		os.Exit(ExitStatusConsumerError)
	}
	err = storage.Init()
	if err != nil {
		log.Println(err)
		os.Exit(ExitStatusConsumerError)
	}
	defer closeStorage(storage)

	brokerCfg := loadBrokerConfiguration()

	// if broker is disabled, simply don't start it
	if !brokerCfg.Enabled {
		log.Println("Broker is disabled, not starting it")
		return
	}

	consumerInstance, err = consumer.New(brokerCfg, storage)
	if err != nil {
		log.Println(err)
		os.Exit(ExitStatusConsumerError)
	}

	defer closeConsumer(consumerInstance)
	consumerInstance.Serve()
}

func startServer() {
	storage, err := startStorageConnection()
	if err != nil {
		os.Exit(ExitStatusServerError)
	}
	defer closeStorage(storage)

	serverCfg := loadServerConfiguration()
	serverInstance = server.New(serverCfg, storage)
	err = serverInstance.Start()
	if err != nil {
		log.Println(err)
		os.Exit(ExitStatusServerError)
	}
}

func startRulesUpdater() {
	rulesCfg := loadRulesConfiguration()
	updaterInstance = rules.NewUpdater(rulesCfg)

	if !rulesCfg.CrontabEnabled {
		log.Println("Cron job for updating rules content is disabled.")
		return
	}

	updaterInstance.StartUpdater()
}

func startService() {
	// rules updater runs "cron" jobs in separate goroutines
	startRulesUpdater()
	// consumer is run in its own thread
	go startConsumer()
	// server can be started in current thread
	startServer()
	os.Exit(ExitStatusOK)
}

func waitForServiceToStart() {
	for {
		isStarted := true
		if viper.Sub("broker").GetBool("enabled") && consumerInstance == nil {
			isStarted = false
		}
		if serverInstance == nil {
			isStarted = false
		}

		if isStarted {
			// everything was initialized
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func stopService() {
	errCode := 0

	if serverInstance != nil {
		err := serverInstance.Stop(context.TODO())
		if err != nil {
			log.Println(err)
			errCode++
		}
	}

	if consumerInstance != nil {
		err := consumerInstance.Close()
		if err != nil {
			log.Println(err)
			errCode++
		}
	}

	os.Exit(errCode)
}

func main() {
	loadConfiguration("config")

	startService()
}
