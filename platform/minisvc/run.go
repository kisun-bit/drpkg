package minisvc

import (
	"fmt"
	"os"
	"strings"

	"github.com/kardianos/service"
)

func Run(opts Options) error {

	svc := &miniService{
		options: opts,
	}

	command := getCommand()

	switch command {

	case "version":

		_, _ = fmt.Fprintln(
			os.Stdout,
			opts.Version,
		)

		return nil

	case "restart":

		if err := invoke(
			opts.Lifecycle.Restart,
		); err != nil {
			return err
		}

	case "install":

		if err := invoke(
			opts.Lifecycle.Install,
		); err != nil {
			return err
		}

	case "uninstall":

		if err := invoke(
			opts.Lifecycle.Uninstall,
		); err != nil {
			return err
		}

	}

	instance, err := service.New(
		svc,
		&opts.Config,
	)

	if err != nil {
		return err
	}

	if isControlCommand(command) {

		return service.Control(
			instance,
			command,
		)
	}

	return instance.Run()
}

func getCommand() string {

	if len(os.Args) < 2 {
		return ""
	}

	return strings.ToLower(
		os.Args[1],
	)
}

func isControlCommand(cmd string) bool {

	switch cmd {

	case "start",
		"stop",
		"restart",
		"install",
		"uninstall":

		return true
	}

	return false
}
