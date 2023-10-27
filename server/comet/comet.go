package comet

import (
	"context"
	"fmt"

	"cosmossdk.io/log"
	abciserver "github.com/cometbft/cometbft/abci/server"
	cmtcfg "github.com/cometbft/cometbft/config"
	cometservice "github.com/cometbft/cometbft/libs/service"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	cometlog "github.com/cosmos/cosmos-sdk/server/comet/log"
	"github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/spf13/cobra"
)

type HasCometBFTServer interface {
	// RegisterTendermintService registers the gRPC Query service for CometBFT queries.
	RegisterTendermintService(client.Context)
}

type Config struct {
	MinGasPrices   string
	QueryGasLimit  uint64
	FlagHaltHeight uint64
	FlagHaltTime   uint64

	Transport  string
	Addr       string
	App        types.Application
	Logger     log.Logger
	Standalone bool

	CmtConfig *cmtcfg.Config
}

type CometBFTServer struct {
	Node *node.Node

	config    Config
	service   cometservice.Service
	cleanupFn func()
}

func New(cfg Config) *CometBFTServer {
	return &CometBFTServer{
		config: cfg,
	}
}

func (s *CometBFTServer) Config(config Config) error {
	return nil
}

func (s *CometBFTServer) Start(ctx context.Context) error {
	// lazyyy TODO: refactor this to get the client and server ctx from context.Context directly
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	clientCtx := client.GetClientContextFromCmd(cmd)
	// serverCtx := server.GetServerContextFromCmd(cmd)

	if cometBftSvr, ok := s.config.App.(HasCometBFTServer); ok {
		cometBftSvr.RegisterTendermintService(clientCtx)
	}

	cmtApp := NewCometABCIWrapper(s.config.App)

	if s.config.Standalone {
		svr, err := abciserver.NewServer(s.config.Addr, s.config.Transport, cmtApp)
		if err != nil {
			return fmt.Errorf("error creating listener: %w", err)
		}

		svr.SetLogger(cometlog.CometLoggerWrapper{Logger: s.config.Logger})

		return svr.Start()
	}

	nodeKey, err := p2p.LoadOrGenNodeKey(s.config.CmtConfig.NodeKeyFile())
	if err != nil {
		return err
	}

	s.Node, err = node.NewNodeWithContext(
		ctx,
		s.config.CmtConfig,
		pvm.LoadOrGenFilePV(s.config.CmtConfig.PrivValidatorKeyFile(), s.config.CmtConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(cmtApp),
		getGenDocProvider(s.config.CmtConfig),
		cmtcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(s.config.CmtConfig.Instrumentation),
		cometlog.CometLoggerWrapper{Logger: s.config.Logger},
	)
	if err != nil {
		return err
	}

	s.cleanupFn = func() {
		if s.Node != nil && s.Node.IsRunning() {
			_ = s.Node.Stop()
		}
	}

	return s.Node.Start()
}

func (s *CometBFTServer) Stop() error {
	defer s.cleanupFn()
	if s.service != nil {
		return s.service.Stop()
	}
	return nil
}

// returns a function which returns the genesis doc from the genesis file.
func getGenDocProvider(cfg *cmtcfg.Config) func() (*cmttypes.GenesisDoc, error) {
	return func() (*cmttypes.GenesisDoc, error) {
		appGenesis, err := genutiltypes.AppGenesisFromFile(cfg.GenesisFile())
		if err != nil {
			return nil, err
		}

		return appGenesis.ToGenesisDoc()
	}
}
