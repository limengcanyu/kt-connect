package command

import (
	"context"
	"errors"
	"fmt"
	"github.com/alibaba/kt-connect/pkg/common"
	"github.com/alibaba/kt-connect/pkg/kt"
	"github.com/alibaba/kt-connect/pkg/kt/command/general"
	"github.com/alibaba/kt-connect/pkg/kt/command/mesh"
	"github.com/alibaba/kt-connect/pkg/kt/options"
	"github.com/alibaba/kt-connect/pkg/kt/process"
	"github.com/alibaba/kt-connect/pkg/kt/util"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	urfave "github.com/urfave/cli"
	"os"
	"time"
)

// NewMeshCommand return new mesh command
func NewMeshCommand(cli kt.CliInterface, options *options.DaemonOptions, action ActionInterface) urfave.Command {
	return urfave.Command{
		Name:  "mesh",
		Usage: "redirect marked requests of specified kubernetes service to local",
		UsageText: "ktctl mesh <service-name> [command options]",
		Flags: general.MeshActionFlag(options),
		Action: func(c *urfave.Context) error {
			if options.Debug {
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
			}
			if err := general.CombineKubeOpts(options); err != nil {
				return err
			}

			if len(c.Args()) == 0 {
				return errors.New("name of deployment to mesh is required")
			}
			if len(options.MeshOptions.Expose) == 0 {
				return errors.New("--expose is required")
			}

			return action.Mesh(c.Args().First(), cli, options)
		},
	}
}

//Mesh exchange kubernetes workload
func (action *Action) Mesh(resourceName string, cli kt.CliInterface, options *options.DaemonOptions) error {
	ch, err := general.SetupProcess(cli, options, common.ComponentMesh)
	if err != nil {
		return err
	}

	if port := util.FindBrokenPort(options.MeshOptions.Expose); port != "" {
		return fmt.Errorf("no application is running on port %s", port)
	}

	ctx := context.Background()
	if options.MeshOptions.Mode == common.MeshModeManual {
		err = mesh.ManualMesh(ctx, cli.Kubernetes(), resourceName, options)
	} else if options.MeshOptions.Mode == common.MeshModeAuto {
		err = mesh.AutoMesh(ctx, cli.Kubernetes(), resourceName, options)
	} else {
		err = fmt.Errorf("invalid mesh method '%s', supportted are %s, %s", options.MeshOptions.Mode,
			common.MeshModeAuto, common.MeshModeManual)
	}
	if err != nil {
		return err
	}

	// watch background process, clean the workspace and exit if background process occur exception
	go func() {
		<-process.Interrupt()
		log.Error().Msgf("Command interrupted")
		general.CleanupWorkspace(cli, options)
		os.Exit(0)
	}()

	s := <-ch
	log.Info().Msgf("Terminal Signal is %s", s)
	// when process interrupt by signal, wait a while for resource clean up
	time.Sleep(1 * time.Second)
	return nil
}

