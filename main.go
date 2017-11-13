package main

import (
	"net/http"
	"os"

	api "github.com/Financial-Times/api-endpoint"
	"github.com/Financial-Times/generic-rw-aurora/db"
	"github.com/Financial-Times/generic-rw-aurora/health"
	"github.com/Financial-Times/http-handlers-go/httphandlers"
	status "github.com/Financial-Times/service-status-go/httphandlers"
	"github.com/husobee/vestigo"
	"github.com/jawher/mow.cli"
	"github.com/rcrowley/go-metrics"
	log "github.com/sirupsen/logrus"
)

const (
	systemCode     = "generic-rw-aurora"
	appDescription = "Generic R/W for Aurora"
)

func main() {
	app := cli.App(systemCode, appDescription)

	appSystemCode := app.String(cli.StringOpt{
		Name:   "app-system-code",
		Value:  systemCode,
		Desc:   "System Code of the application",
		EnvVar: "APP_SYSTEM_CODE",
	})

	appName := app.String(cli.StringOpt{
		Name:   "app-name",
		Value:  systemCode,
		Desc:   "Application name",
		EnvVar: "APP_NAME",
	})

	port := app.String(cli.StringOpt{
		Name:   "port",
		Value:  "8080",
		Desc:   "Port to listen on",
		EnvVar: "PORT",
	})

	dbURL := app.String(cli.StringOpt{
		Name:   "db-connection-url",
		Value:  "/pac",
		Desc:   "Database connection URL",
		EnvVar: "DB_CONNECTION_URL",
	})

	performSchemaMigrations := app.Bool(cli.BoolOpt{
		Name:   "db-perform-schema-migrations",
		Value:  false,
		Desc:   "Whether to perform database schema migrations on startup",
		EnvVar: "DB_PERFORM_SCHEMA_MIGRATIONS",
	})

	apiYml := app.String(cli.StringOpt{
		Name:   "api-yml",
		Value:  "./api.yml",
		Desc:   "Location of the API Swagger YML file.",
		EnvVar: "API_YML",
	})

	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.InfoLevel)
	log.Infof("[Startup] %v is starting", *appSystemCode)

	app.Action = func() {
		log.Infof("System code: %s, App Name: %s, Port: %s", *appSystemCode, *appName, *port)

		conn, err := db.Connect(*dbURL)
		if err != nil {
			log.WithError(err).Error("unable to connect to database")
		}

		rw := db.NewService(conn, *performSchemaMigrations)

		healthService := health.NewHealthService(*appSystemCode, *appName, appDescription, rw)

		serveEndpoints(*port, apiYml, rw, healthService)
	}

	err := app.Run(os.Args)
	if err != nil {
		log.WithError(err).Error("App could not start")
	}
}

func serveEndpoints(port string, apiYml *string, db db.AuroraRWService, healthService *health.HealthService) {
	r := vestigo.NewRouter()

	var monitoringRouter http.Handler = r
	monitoringRouter = httphandlers.TransactionAwareRequestLoggingHandler(log.StandardLogger(), monitoringRouter)
	monitoringRouter = httphandlers.HTTPMetricsHandler(metrics.DefaultRegistry, monitoringRouter)

	r.Get("/__health", healthService.HealthCheckHandleFunc())
	r.Get(status.GTGPath, status.NewGoodToGoHandler(healthService.GTG))
	r.Get(status.BuildInfoPath, status.BuildInfoHandler)

	http.Handle("/", monitoringRouter)

	if apiYml != nil {
		apiEndpoint, err := api.NewAPIEndpointForFile(*apiYml)
		if err != nil {
			log.WithError(err).WithField("file", *apiYml).Warn("Failed to serve the API Endpoint for this service. Please validate the Swagger YML and the file location")
		} else {
			r.Get(api.DefaultPath, apiEndpoint.ServeHTTP)
		}
	}

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Unable to start: %v", err)
	}
}
