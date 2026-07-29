package minisvc

import "github.com/kardianos/service"

type Callback func() error

type Lifecycle struct {
	Start Callback

	Stop Callback

	Restart Callback

	Install Callback

	Uninstall Callback
}

type Options struct {
	Version string

	service.Config

	Lifecycle Lifecycle
}
