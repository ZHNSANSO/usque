package cmd

import (
	"log"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:   "service [install|uninstall|start|stop|restart]",
	Short: "Manage the usque service",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help()
			return
		}

		svcConfig := &service.Config{
			Name:        "usque",
			DisplayName: "Usque Service",
			Description: "Usque Warp CLI as a service.",
		}

		prg := &program{}
		s, err := service.New(prg, svcConfig)
		if err != nil {
			log.Fatal(err)
		}

		switch args[0] {
		case "install":
			err = s.Install()
			if err != nil {
				log.Fatalf("Failed to install service: %v", err)
			}
			log.Println("Service installed successfully.")
		case "uninstall":
			err = s.Uninstall()
			if err != nil {
				log.Fatalf("Failed to uninstall service: %v", err)
			}
			log.Println("Service uninstalled successfully.")
		case "start":
			err = s.Start()
			if err != nil {
				log.Fatalf("Failed to start service: %v", err)
			}
			log.Println("Service started successfully.")
		case "stop":
			err = s.Stop()
			if err != nil {
				log.Fatalf("Failed to stop service: %v", err)
			}
			log.Println("Service stopped successfully.")
		case "restart":
			err = s.Restart()
			if err != nil {
				log.Fatalf("Failed to restart service: %v", err)
			}
			log.Println("Service restarted successfully.")
		default:
			cmd.Help()
		}
	},
}

type program struct{}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	// This is where the main logic for the service will go.
	// For now, it will just log a message.
	// Later, it will start the tunnel and the web UI.
	log.Println("Usque service is running...")
	// In the future, this will call a function to start the tunnel and web UI.
	// e.g., web.StartServer()
	select {} // Block forever
}

func (p *program) Stop(s service.Service) error {
	// Stop should not block. Return with a few seconds.
	log.Println("Usque service is stopping...")
	return nil
}

func init() {
	rootCmd.AddCommand(serviceCmd)
}
