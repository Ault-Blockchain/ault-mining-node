package api

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cmthttp "github.com/cometbft/cometbft/rpc/client/http"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	minertypes "github.com/Ault-Blockchain/ault/x/miner/types"
)

type ChainReady struct {
	OK     bool   `json:"ok"`
	GRPCOK bool   `json:"grpc_ok"`
	RPCOK  bool   `json:"rpc_ok"`
	Height int64  `json:"height"`
	Epoch  uint64 `json:"epoch"`
	Error  string `json:"error,omitempty"`
}

func checkRpc(ctx context.Context) ChainReady {
	res := ChainReady{}
	cfg := config.Get()
	grpcEndpoint := cfg.GRPCEndpoint
	rpcEndpoint := cfg.RPCEndpoint

	// gRPC epoch check
	gctx, gcancel := context.WithTimeout(ctx, 2*time.Second)
	defer gcancel()
	conn, err := grpc.DialContext(gctx, grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials())) //nolint:staticcheck // DialContext is supported throughout 1.x
	if err == nil {
		q := minertypes.NewQueryClient(conn)
		if e, eerr := q.Epoch(gctx, &minertypes.QueryEpochRequest{}); eerr == nil && e != nil {
			res.GRPCOK = true
			res.Epoch = e.Epoch
		} else if eerr != nil {
			res.Error = eerr.Error()
		}
		_ = conn.Close()
	} else {
		res.Error = err.Error()
	}

	// RPC height check
	rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
	defer rcancel()
	cli, err := cmthttp.New(rpcEndpoint, "/websocket")
	if err == nil {
		if st, serr := cli.Status(rctx); serr == nil {
			res.RPCOK = true
			res.Height = st.SyncInfo.LatestBlockHeight
		} else {
			res.Error = serr.Error()
		}
	} else {
		res.Error = err.Error()
	}

	res.OK = res.GRPCOK && res.RPCOK
	return res
}
