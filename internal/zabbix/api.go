package zabbix

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	sender "github.com/christos-diamantis/golang-zabbix-sender"
	zabbixapi "github.com/kgeroczi/go-zabbix-api"
)

type TrapperSender struct {
	addr   string
	sender *sender.Sender
}

func NewTrapperSender(addr string) *TrapperSender {
	return &TrapperSender{
		addr:   addr,
		sender: sender.NewSenderTimeout(addr, 5*time.Second, 15*time.Second, 15*time.Second),
	}
}

func (s *TrapperSender) SendMetrics(metrics []*sender.Metric) error {
	_, errActive, resTrapper, errTrapper := s.sender.SendMetrics(metrics)
	if errTrapper != nil {
		return errTrapper
	}
	if errActive != nil {
		return errActive
	}
	if resTrapper.Response != "" && resTrapper.Response != "success" {
		return fmt.Errorf("zabbix rejected data: %s", resTrapper.Info)
	}
	return nil
}

func Login(apiURL, user, pass, apiKey string) (*zabbixapi.API, error) {
	api, err := zabbixapi.NewAPI(zabbixapi.Config{Url: apiURL})
	if err != nil {
		return nil, fmt.Errorf("error initializing API: %w", err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	api.SetClient(&http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	})

	if apiKey != "" {
		_, err = api.Token(apiKey)
		if err != nil {
			return nil, fmt.Errorf("error injecting api key: %w", err)
		}
		log.Printf("Using API token for authentication.")
		return api, nil
	}

	_, err = api.Login(user, pass)
	if err != nil {
		return nil, fmt.Errorf("error logging into Zabbix API: %w", err)
	}

	log.Printf("Logged into Zabbix API (user: %s).", user)
	return api, nil
}
