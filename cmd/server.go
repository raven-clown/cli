package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rancher/cli/config"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"
	"golang.org/x/exp/maps"
)

type serverData struct {
	Index   int
	Current string
	Name    string
	URL     string
}

// ServerCommand defines the 'rancher server' sub-commands
func ServerCommand() *cli.Command {
	cfg := &config.Config{}

	return &cli.Command{
		Name:  "server",
		Usage: "Operations for the server",
		Description: `Switch or view the server currently in focus.
`,
		Before: loadAndValidateConfig(cfg),
		Commands: []*cli.Command{
			{
				Name:  "current",
				Usage: "Display the current server",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return serverCurrent(cmd.Root().Writer, cfg)
				},
			},
			{
				Name:      "delete",
				Usage:     "Delete a server from the local config",
				ArgsUsage: "[SERVER_NAME]",
				Description: `
The server arg is optional, if not passed in a list of available servers will
be displayed and one can be selected.
`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					serverName, err := getSelectedServer(cmd, cfg)
					if err != nil {
						return err
					}
					return serverDelete(cfg, serverName)
				},
			},
			{
				Name:      "ls",
				Usage:     "List all servers",
				ArgsUsage: "None",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					format := cmd.String("format")
					return serverLs(cmd.Root().Writer, cfg, format)
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "format",
						Usage: "'json', 'yaml' or Custom format: '{{.Name}} {{.URL}}'",
					},
				},
			},
			{
				Name:      "switch",
				Usage:     "Switch to a new server",
				ArgsUsage: "[SERVER_NAME]",
				Description: `
		The server arg is optional, if not passed in a list of available servers will
		be displayed and one can be selected.
		`,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					serverName, err := getSelectedServer(cmd, cfg)
					if err != nil {
						return err
					}
					return serverSwitch(cfg, serverName)
				},
			},
		},
	}
}

// serverCurrent command to display the name of the current server in the local config
func serverCurrent(out io.Writer, cfg *config.Config) error {
	serverName := cfg.CurrentServer

	currentServer, found := cfg.Servers[serverName]
	if !found {
		return errors.New("current server not set")
	}

	fmt.Fprintf(out, "Name: %s URL: %s\n", serverName, currentServer.URL)
	return nil
}

// serverDelete command to delete a server from the local config
func serverDelete(cfg *config.Config, serverName string) error {
	_, ok := cfg.Servers[serverName]
	if !ok {
		return errors.New("server not found")
	}
	delete(cfg.Servers, serverName)

	if cfg.CurrentServer == serverName {
		cfg.CurrentServer = ""
	}

	err := cfg.Write()
	if err != nil {
		return err
	}
	logrus.Infof("Server %s deleted", serverName)
	return nil
}

// serverLs command to list rancher servers from the local config
func serverLs(out io.Writer, cfg *config.Config, format string) error {
	writerConfig := &TableWriterConfig{
		Writer: out,
		Format: format,
	}

	writer := NewTableWriterWithConfig([][]string{
		{"CURRENT", "Current"},
		{"NAME", "Name"},
		{"URL", "URL"},
	}, writerConfig)

	defer writer.Close()

	servers := getServers(cfg)
	for _, server := range servers {
		writer.Write(server)
	}

	return writer.Err()
}

// serverSwitch will alter and write the config to switch rancher server.
func serverSwitch(cf *config.Config, serverName string) error {
	_, ok := cf.Servers[serverName]
	if !ok {
		return errors.New("server not found")
	}

	if len(cf.Servers[serverName].Project) == 0 {
		logrus.Warn("No context set; some commands will not work. Run 'rancher context switch'")
	}

	cf.CurrentServer = serverName

	err := cf.Write()
	if err != nil {
		return err
	}
	return nil
}

// getSelectedServer will get the selected server if provided as argument,
// or it will prompt the user to select one.
func getSelectedServer(cmd *cli.Command, cfg *config.Config) (string, error) {
	serverName := cmd.Args().First()
	if serverName != "" {
		return serverName, nil
	}
	return serverFromInput(cmd, cfg)
}

// serverFromInput displays the list of servers from the local config and
// prompt the user to select one.
func serverFromInput(cmd *cli.Command, cf *config.Config) (string, error) {
	servers := getServers(cf)
	if len(servers) == 0 {
		return "", errors.New("no servers found in the local config")
	}

	if err := displayListServers(cmd, servers); err != nil {
		return "", err
	}

	selection, err := readServerSelection(os.Stdin, os.Stdout, len(servers))
	if err != nil {
		return "", err
	}
	return servers[selection].Name, nil
}

// readServerSelection prompts until the user enters a number between 1 and
// count, and returns it as a zero-based index. Empty or invalid input prints
// an error and asks again.
func readServerSelection(in io.Reader, out io.Writer, count int) (int, error) {
	fmt.Fprint(out, "Select a Server: ")
	reader := bufio.NewReader(in)

	errMessage := fmt.Sprintf("Invalid input, enter a number between 1 and %v: ", count)

	for {
		input, err := reader.ReadString('\n')
		i, convErr := strconv.Atoi(strings.TrimSpace(input))
		if convErr == nil && i >= 1 && i <= count {
			return i - 1, nil
		}
		if errors.Is(err, io.EOF) {
			return 0, errors.New("no server selected")
		}
		if err != nil {
			return 0, err
		}
		fmt.Fprint(out, errMessage)
	}
}

// displayListServers displays the list of rancher servers
func displayListServers(cmd *cli.Command, servers []*serverData) error {
	writer := NewTableWriter([][]string{
		{"INDEX", "Index"},
		{"NAME", "Name"},
		{"URL", "URL"},
	}, cmd)

	defer writer.Close()

	for _, server := range servers {
		writer.Write(server)
	}
	return writer.Err()
}

// getServers returns an ordered slice (by name) of serverData
func getServers(cfg *config.Config) []*serverData {
	serverNames := maps.Keys(cfg.Servers)
	sort.Strings(serverNames)

	servers := []*serverData{}

	for i, server := range serverNames {
		var current string
		if server == cfg.CurrentServer {
			current = "*"
		}

		servers = append(servers, &serverData{
			Index:   i + 1,
			Name:    server,
			Current: current,
			URL:     cfg.Servers[server].URL,
		})
	}

	return servers
}

func loadAndValidateConfig(cfg *config.Config) cli.BeforeFunc {
	return func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		conf, err := loadConfig(cmd)
		if err != nil {
			return ctx, err
		}
		*cfg = conf

		if len(cfg.Servers) == 0 {
			return ctx, errors.New("no servers are currently configured")
		}
		return ctx, nil
	}
}
