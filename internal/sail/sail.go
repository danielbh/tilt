package sail

import (
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"
)

const DefaultPort = 10450
const DefaultWebDevPort = 10451

var port = 0
var webDevPort = 0

func Execute() {
	rootCmd := &cobra.Command{
		Use:   "sail",
		Short: "A server to coordinate collaborative coding and debugging with Tilt",
		Run:   run,
	}
	rootCmd.Flags().IntVar(&port, "port", DefaultPort, "Port to listen on")
	rootCmd.Flags().IntVar(&webDevPort, "webdev-port", DefaultWebDevPort, "Port for the web dev server to listen on")

	err := rootCmd.Execute()
	if err != nil {
		log.Fatal(err)
	}
}

func run(cmd *cobra.Command, args []string) {
	server := ProvideSailServer()
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: http.DefaultServeMux,
	}
	http.Handle("/", server.Router())

	log.Printf("Sail server listening on %d\n", port)
	err := httpServer.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}
