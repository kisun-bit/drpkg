package minisvc

import (
	"fmt"

	"github.com/kardianos/service"
)

type miniService struct {
	options Options
}

func (m *miniService) Start(_ service.Service) error {

	go func() {

		if err := invoke(m.options.Lifecycle.Start); err != nil {
			fmt.Println("service start error:", err)
		}

	}()

	return nil
}

func (m *miniService) Stop(_ service.Service) error {

	return invoke(
		m.options.Lifecycle.Stop,
	)

}

func invoke(cb Callback) error {

	if cb == nil {
		return nil
	}

	return cb()
}
