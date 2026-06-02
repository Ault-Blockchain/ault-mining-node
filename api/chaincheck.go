package api

import (
	"context"
	"strings"
	"time"

	cmthttp "github.com/cometbft/cometbft/rpc/client/http"

	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/client"
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
	rpcEndpoints := splitEndpoints(cfg.RPCEndpoint)

	// gRPC epoch check (honors CHAIN_GRPC_TLS: TLS-first then plaintext fallback)
	if epoch, err := client.CheckGRPCEpoch(ctx, 2*time.Second); err == nil {
		res.GRPCOK = true
		res.Epoch = epoch
	} else {
		res.Error = err.Error()
	}

	// RPC height check
	for _, rpcEndpoint := range rpcEndpoints {
		rctx, rcancel := context.WithTimeout(ctx, 2*time.Second)
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
		rcancel()
		if res.RPCOK {
			break
		}
	}

	res.OK = res.GRPCOK && res.RPCOK
	return res
}

func splitEndpoints(raw string) []string {
	parts := strings.Split(raw, ",")
	endpoints := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			endpoints = append(endpoints, part)
		}
	}
	return endpoints
}
