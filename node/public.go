// Package node provides a high level wrapper around tendermint services.
package node

import (
	"context"
	"fmt"

	abciclient "github.com/ari-anchor/sei-tendermint/abci/client"
	"github.com/ari-anchor/sei-tendermint/config"
	"github.com/ari-anchor/sei-tendermint/libs/log"
	"github.com/ari-anchor/sei-tendermint/libs/service"
	"github.com/ari-anchor/sei-tendermint/privval"
	"github.com/ari-anchor/sei-tendermint/types"
	"go.opentelemetry.io/otel/sdk/trace"
)

// NewDefault constructs a tendermint node service for use in go
// process that host their own process-local tendermint node. This is
// equivalent to running tendermint in it's own process communicating
// to an external ABCI application.
func NewDefault(
	ctx context.Context,
	conf *config.Config,
	logger log.Logger,
	restartCh chan struct{},
) (service.Service, error) {
	return newDefaultNode(ctx, conf, logger, restartCh)
}

// New constructs a tendermint node. The ClientCreator makes it
// possible to construct an ABCI application that runs in the same
// process as the tendermint node.  The final option is a pointer to a
// Genesis document: if the value is nil, the genesis document is read
// from the file specified in the config, and otherwise the node uses
// value of the final argument.
func New(
	ctx context.Context,
	conf *config.Config,
	logger log.Logger,
	restartCh chan struct{},
	cf abciclient.Client,
	gen *types.GenesisDoc,
	tracerProviderOptions []trace.TracerProviderOption,
	nodeMetrics *NodeMetrics,
) (service.Service, error) {
	nodeKey, err := types.LoadOrGenNodeKey(conf.NodeKeyFile())
	if err != nil {
		return nil, fmt.Errorf("failed to load or gen node key %s: %w", conf.NodeKeyFile(), err)
	}

	var genProvider genesisDocProvider
	switch gen {
	case nil:
		genProvider = defaultGenesisDocProviderFunc(conf)
	default:
		genProvider = func() (*types.GenesisDoc, error) { return gen, nil }
	}

	switch conf.Mode {
	case config.ModeFull, config.ModeValidator:
		pval, err := privval.LoadOrGenFilePV(conf.PrivValidator.KeyFile(), conf.PrivValidator.StateFile())
		if err != nil {
			return nil, err
		}

		return makeNode(
			ctx,
			conf,
			restartCh,
			pval,
			nodeKey,
			cf,
			genProvider,
			config.DefaultDBProvider,
			logger,
			tracerProviderOptions,
			nodeMetrics,
		)
	case config.ModeSeed:
		return makeSeedNode(
			ctx,
			logger,
			conf,
			restartCh,
			config.DefaultDBProvider,
			nodeKey,
			genProvider,
			cf,
			nodeMetrics,
		)
	default:
		return nil, fmt.Errorf("%q is not a valid mode", conf.Mode)
	}
}
