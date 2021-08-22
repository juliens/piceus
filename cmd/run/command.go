package run

import (
	"github.com/ettle/strcase"
	"github.com/urfave/cli/v2"
)

const (
	flagGitHubToken         = "github-token"
	flagServicesAccessToken = "services-access-token"
	flagPluginURL           = "plugin-url"
)

const (
	flagTracingEndpoint    = "tracing-endpoint"
	flagTracingUsername    = "tracing-username"
	flagTracingPassword    = "tracing-password"
	flagTracingProbability = "tracing-probability"
)

// Command creates the run command.
func Command() *cli.Command {
	cmd := &cli.Command{
		Name:        "run",
		Usage:       "Run Piceus",
		Description: "Launch application piceus",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     flagGitHubToken,
				Usage:    "GitHub Token.",
				EnvVars:  []string{strcase.ToSNAKE(flagGitHubToken)},
				Required: true,
			},
			&cli.StringFlag{
				Name:     flagServicesAccessToken,
				Usage:    "Pilot Services Access Token",
				EnvVars:  []string{"PILOT_" + strcase.ToSNAKE(flagServicesAccessToken)},
				Required: true,
			},
			&cli.StringFlag{
				Name:     flagPluginURL,
				Usage:    "Plugin Service URL",
				EnvVars:  []string{"PILOT_" + strcase.ToSNAKE(flagPluginURL)},
				Required: true,
			},
		},
		Action: func(cliCtx *cli.Context) error {
			cfg := buildConfig(cliCtx)

			return run(cliCtx.Context, cfg)
		},
	}

	cmd.Flags = append(cmd.Flags, tracingFlags()...)

	return cmd
}

func tracingFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     flagTracingEndpoint,
			Usage:    "Endpoint to send traces",
			EnvVars:  []string{strcase.ToSNAKE(flagTracingEndpoint)},
			Value:    "https://collector.infra.traefiklabs.tech",
			Required: false,
		},
		&cli.StringFlag{
			Name:     flagTracingUsername,
			Usage:    "Username to connect to Jaeger",
			EnvVars:  []string{strcase.ToSNAKE(flagTracingUsername)},
			Value:    "jaeger",
			Required: false,
		},
		&cli.StringFlag{
			Name:     flagTracingPassword,
			Usage:    "Password to connect to Jaeger",
			EnvVars:  []string{strcase.ToSNAKE(flagTracingPassword)},
			Value:    "jaeger",
			Required: false,
		},
		&cli.Float64Flag{
			Name:     flagTracingProbability,
			Usage:    "Probability to send traces.",
			EnvVars:  []string{strcase.ToSNAKE(flagTracingProbability)},
			Value:    0,
			Required: false,
		},
	}
}
