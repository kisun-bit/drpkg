package main

import (
	"fmt"
	"os"
	"time"

	"github.com/kardianos/service"

	"github.com/kisun-bit/drpkg/platform/minisvc"
)

type Agent struct {
	stop chan struct{}
}

func NewAgent() *Agent {

	return &Agent{
		stop: make(chan struct{}),
	}
}

func (a *Agent) Run() {

	fmt.Println("agent starting...")

	ticker := time.NewTicker(
		time.Second,
	)

	defer ticker.Stop()

	for {

		select {

		case <-ticker.C:

			fmt.Println(
				"agent running...",
			)

		case <-a.stop:

			fmt.Println(
				"agent goroutine exit",
			)

			return
		}
	}
}

func (a *Agent) Stop() error {

	fmt.Println(
		"agent stopping...",
	)

	close(a.stop)

	return nil
}

func main() {

	agent := NewAgent()

	err := minisvc.Run(
		minisvc.Options{

			Version: "1.0.0",

			Config: service.Config{

				Name: "demo-agent",

				DisplayName: "Demo Agent",

				Description: "A minimal golang service demo",
			},

			Lifecycle: minisvc.Lifecycle{

				Start: func() error {

					agent.Run()

					return nil

				},

				Stop: func() error {

					return agent.Stop()

				},

				Restart: func() error {

					fmt.Println(
						"restart callback",
					)

					return nil

				},

				Install: func() error {

					fmt.Println(
						"prepare install",
					)

					return nil

				},

				Uninstall: func() error {

					fmt.Println(
						"prepare uninstall",
					)

					return nil

				},
			},
		},
	)

	if err != nil {

		fmt.Println(
			"service error:",
			err,
		)

		os.Exit(1)
	}
}
