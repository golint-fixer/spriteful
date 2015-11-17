package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"os/signal"

	"github.com/Sirupsen/logrus"
	"github.com/emicklei/go-restful"
)

// These are the error codes returned.
const (
	ExitLoadConfigError = iota
	ExitParseConfigError
)

type (
	// Spriteful handles the API endpoints.
	Spriteful struct {
		BindHost   string   `json:"bind-host"`
		BindPort   int      `json:"bind-port"`
		Repository string   `json:"repository"`
		Servers    []Server `json:"servers"`
	}

	// Server represents a server with it's boot configuration.
	Server struct {
		MacAddress  string                 `json:"mac"`
		Kernel      string                 `json:"kernel"`
		Initrd      []string               `json:"initrd"`
		CommandLine map[string]interface{} `json:"cmdline"`
	}

	// PixieResponse is the response required by pixie core for booting up servers.
	PixieResponse struct {
		Kernel      string                 `json:"kernel"`
		Initrd      []string               `json:"initrd"`
		CommandLine map[string]interface{} `json:"cmdline"`
	}
)

// main starts Spriteful API using the provided configuration.
func main() {
	logrus.Info("Starting Spriteful API...")
	config := flag.String("config", "config.json", "spriteful configuration")
	flag.Parse()
	data, err := ioutil.ReadFile(*config)
	if err != nil {
		logrus.WithField(logrus.ErrorKey, err).Fatal("unable to read config")
		os.Exit(ExitLoadConfigError)
	}
	var sprite Spriteful
	if err := json.Unmarshal(data, &sprite); err != nil {
		logrus.WithField(logrus.ErrorKey, err).Fatal("unable to parse config.")
		os.Exit(ExitParseConfigError)
	}
	logrus.Infof(`Config "%s" loaded.`, *config)

	sprite.startApi()
}

// startApi starts the Spriteful API.
func (s *Spriteful) startApi() {
	container := restful.NewContainer()
	s.register(container)

	bindAddress := net.JoinHostPort(s.BindHost, strconv.Itoa(s.BindPort))
	server := &http.Server{
		Addr:    bindAddress,
		Handler: container,
	}
	go server.ListenAndServe()
	logrus.Infof(`Spriteful API now listening at "%s".`, bindAddress)

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-ch
	logrus.Info("Shutting down Spriteful API...")
}

// register registers the endpoints for the API.
func (s *Spriteful) register(container *restful.Container) {
	logrus.Info("Creating API endpoints...")

	ws := &restful.WebService{}
	ws.Path("/api/v1")

	ws.Route(ws.GET("boot/{mac-addr}").To(s.handleBootRequest).
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON).
		Param(ws.PathParameter("mac-addr", "the mac address")).
		Writes(PixieResponse{}))
	logrus.Info(`pixiecore endpoint created at "api/v1/boot/{mac}".`)

	ws.Route(ws.GET("/static/{resource:*}").To(s.handleResourceRequest).
		Param(ws.PathParameter("resource", "the resource file")))
	logrus.Info(`static endpoint created at "api/v1/static/{.*}".`)

	container.Add(ws)
}

// handleBootRequest handles the http request for server boot configuration.
func (s *Spriteful) handleBootRequest(req *restful.Request, res *restful.Response) {
	logrus.Info("Received pixiecore request...")
	macAddress := req.PathParameter("mac-addr")
	server, err := s.findServerConfig(macAddress)
	if err != nil {
		res.WriteError(http.StatusNotFound, err)
	} else {
		res.WriteEntity(&PixieResponse{
			Kernel:      server.Kernel,
			Initrd:      server.Initrd,
			CommandLine: server.CommandLine,
		})
	}
}

// handleResourceRequest handles the http request for static  resources.
func (s *Spriteful) handleResourceRequest(req *restful.Request, res *restful.Response) {
	logrus.Info("Received resource request...")
	resource := req.PathParameter("resource")

	resourcePath, err := s.findResource(resource)
	if err != nil {
		res.WriteError(http.StatusNotFound, err)
	} else {
		http.ServeFile(res.ResponseWriter, req.Request, resourcePath)
	}
}

// findServerConfig returns the server config for the requested MAC address.
// Returns an error if no configuration is found.
func (s *Spriteful) findServerConfig(macAddress string) (*Server, error) {
	logrus.Infof(`requesting configuration for server "%s".`, macAddress)
	for _, server := range s.Servers {
		if strings.EqualFold(macAddress, server.MacAddress) {
			logrus.Info("configuration found.")
			return &server, nil
		}
	}
	logrus.Warn("configuration not found.")
	return nil, errors.New(fmt.Sprintf("no configuration defined for %s.", macAddress))
}

// findResource returns the full resource path if the requested resource exists.
// Returns an error if the resource does not exist.
func (s *Spriteful) findResource(resource string) (string, error) {
	logrus.Info(`requesting resource "%s".`, resource)
	resourcePath := path.Join(s.Repository, resource)
	if _, err := os.Stat(resourcePath); os.IsNotExist(err) {
		logrus.Warn("resource does not exist.")
		return "", errors.New(fmt.Sprintf("resource does not exist at %s.", resourcePath))
	}
	logrus.Info("resource found.")
	return resourcePath, nil
}
