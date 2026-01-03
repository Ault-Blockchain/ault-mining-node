package api

import (
	"context"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/Ault-Blockchain/ault-miner-node/internal/storage"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/client"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/mining"
)

// RewardResponse represents the response for /v1/rewards endpoint
type RewardResponse struct {
	LicenseID    uint64 `json:"license_id"`
	TotalPayout  string `json:"total_payout"`
	TotalCredits uint64 `json:"total_credits"`
	FromEpoch    uint64 `json:"from_epoch"`
	ToEpoch      uint64 `json:"to_epoch"`
	Source       string `json:"source"` // "db" or "chain"
}

// AutoModeStateGetter is a function that returns the current auto mode state
type AutoModeStateGetter func() interface{}

type Server struct {
	app              *fiber.App
	db               *storage.DB
	stats            func() interface{}
	chainClient      *client.ChainClient
	autoModeState    AutoModeStateGetter
	mu               sync.RWMutex
}

func New(db *storage.DB, statsFunc func() interface{}, chainClient *client.ChainClient) *Server {
	return &Server{
		app:         fiber.New(fiber.Config{DisableStartupMessage: true}),
		db:          db,
		stats:       statsFunc,
		chainClient: chainClient,
	}
}

// SetManager updates the server with the miner manager's db and stats after initialization
func (s *Server) SetManager(db *storage.DB, statsFunc func() interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
	s.stats = statsFunc
}

// SetAutoModeState sets the function to retrieve auto mode state
func (s *Server) SetAutoModeState(getter AutoModeStateGetter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoModeState = getter
}

func (s *Server) mountRoutes() {
	s.app.Use(fiberrecover.New())

	// Health check (DB + rpc status)
	s.app.Get("/health", func(c *fiber.Ctx) error {
		s.mu.RLock()
		db := s.db
		s.mu.RUnlock()

		dbOK := true      // for health check: true if db disabled or working
		dbAvailable := false // for reporting: true only if db enabled and working
		if db != nil {
			_, err := db.RecentSubmissions(c.Context(), 1)
			dbAvailable = err == nil
			dbOK = dbAvailable
		}
		cr := checkRpc(c.Context())
		ok := dbOK && cr.OK
		out := fiber.Map{"ok": ok, "db": dbAvailable, "chain": cr.OK}
		if cr.Epoch > 0 {
			out["epoch"] = cr.Epoch
		}
		code := fiber.StatusOK
		if !ok {
			code = fiber.StatusServiceUnavailable
		}
		return c.Status(code).JSON(out)
	})

	v1 := s.app.Group("/v1")

	// Miner status + stats
	v1.Get("/status", func(c *fiber.Ctx) error {
		s.mu.RLock()
		stats := s.stats
		s.mu.RUnlock()

		status := fiber.Map{"running": true}
		if stats != nil {
			status["stats"] = stats()
		}
		return c.JSON(status)
	})

	// Submissions list
	v1.Get("/submissions", func(c *fiber.Ctx) error {
		s.mu.RLock()
		db := s.db
		s.mu.RUnlock()

		if db == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "database disabled")
		}
		var f storage.SubmissionFilter
		if v := c.Query("license_id"); v != "" {
			if id, err := strconv.ParseUint(v, 10, 64); err == nil {
				f.LicenseID = &id
			}
		}
		if v := c.Query("epoch"); v != "" {
			if e, err := strconv.ParseUint(v, 10, 64); err == nil {
				f.Epoch = &e
			}
		}
		f.Limit = c.QueryInt("limit", 100)
		f.Offset = c.QueryInt("offset", 0)
		subs, err := db.ListSubmissions(c.Context(), f)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(subs)
	})

	// Rewards query - returns total payout and credits for a license
	// Query params:
	//   - license_id (required): license ID to query
	//   - source: "db" (from local DB) or "chain" (default, from chain via gRPC)
	v1.Get("/rewards", func(c *fiber.Ctx) error {
		s.mu.RLock()
		db := s.db
		stats := s.stats
		s.mu.RUnlock()

		licenseIDStr := c.Query("license_id")
		if licenseIDStr == "" {
			return fiber.NewError(fiber.StatusBadRequest, "license_id is required")
		}
		licenseID, err := strconv.ParseUint(licenseIDStr, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid license_id")
		}

		// Get epoch range from stats
		var startEpoch, lastProcessedEpoch uint64
		if stats != nil {
			if st, ok := stats().(mining.MiningStats); ok {
				startEpoch = st.StartEpoch
				lastProcessedEpoch = st.LastProcessedEpoch
			}
		}

		source := c.Query("source", "chain")

		if source == "db" {
			if db == nil {
				return fiber.NewError(fiber.StatusServiceUnavailable, "database disabled")
			}
			// Query from local DB (settled rewards only)
			payout, credits, err := db.GetTotalEarnedRewards(c.Context(), licenseID)
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, err.Error())
			}
			return c.JSON(RewardResponse{
				LicenseID:    licenseID,
				TotalPayout:  payout,
				TotalCredits: credits,
				FromEpoch:    startEpoch,
				ToEpoch:      lastProcessedEpoch,
				Source:       "db",
			})
		}

		// Query from chain via gRPC (from start_epoch to last_processed_epoch)
		if s.chainClient == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "chain client not available")
		}
		resp, err := s.chainClient.GetLicensePayouts(c.Context(), licenseID, startEpoch, lastProcessedEpoch)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		return c.JSON(RewardResponse{
			LicenseID:    licenseID,
			TotalPayout:  resp.TotalPayout.String(),
			TotalCredits: resp.TotalCredits,
			FromEpoch:    startEpoch,
			ToEpoch:      lastProcessedEpoch,
			Source:       "chain",
		})
	})

	// Operator info - returns operator address and status (for auto mode / fly.io)
	v1.Get("/operator", func(c *fiber.Ctx) error {
		s.mu.RLock()
		autoModeState := s.autoModeState
		s.mu.RUnlock()

		if autoModeState == nil {
			// Not in auto mode, return basic info from chain client
			if s.chainClient == nil {
				return fiber.NewError(fiber.StatusServiceUnavailable, "chain client not available")
			}
			ownerAddr, err := s.chainClient.GetOwnerAddress()
			if err != nil {
				return fiber.NewError(fiber.StatusInternalServerError, err.Error())
			}
			evmAddr := common.BytesToAddress([]byte(ownerAddr))
			// Return same schema as auto mode for API consistency
			return c.JSON(fiber.Map{
				"operator_address": ownerAddr.String(),
				"evm_address":      evmAddr.Hex(),
				"vrf_pub_key_hex":  "",
				"vrf_registered":   false,
				"license_count":    0,
				"licenses":         []uint64{},
				"status":           "manual",
				"next_step":        "",
				"auto_mode":        false,
			})
		}

		// Auto mode - return full state
		state := autoModeState()
		if state == nil {
			return fiber.NewError(fiber.StatusServiceUnavailable, "auto mode state not available")
		}
		return c.JSON(state)
	})
}

func (s *Server) Start(addr string) error {
	s.mountRoutes()
	if addr == "" {
		addr = ":8080"
	}
	return s.app.Listen(addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.app.ShutdownWithContext(ctx); err != nil {
		return err
	}
	if s.db != nil {
		_ = s.db.Close()
	}
	return nil
}
