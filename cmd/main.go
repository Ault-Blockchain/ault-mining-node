package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ProtonMail/go-ecvrf/ecvrf"
	"github.com/spf13/cobra"

	sdk "github.com/cosmos/cosmos-sdk/types"

	appcfg "github.com/Ault-Blockchain/ault/app/config"
	"github.com/Ault-Blockchain/ault-miner-node/api"
	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/client"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/mining"
)

var rootCmd = &cobra.Command{
	Use:   "aultmined",
	Short: "Ault mining client",
	Long:  `A mining client for the Ault x/miner module that performs VRF-based mining with micro proof-of-work.`,
}

func init() {
	cobra.OnInitialize(config.Load)

	// Set Ault bech32 prefix from app config
	sdkConfig := sdk.GetConfig()
	appcfg.SetBech32Prefixes(sdkConfig)
	sdkConfig.Seal()

	// Add subcommands
	rootCmd.AddCommand(
		vrfKeygenCmd(),
		setKeyCmd(),
		mineCmd(),
	)
}

func vrfKeygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vrfkeygen",
		Short: "Generate a new VRF keypair",
		Long: `Generate a new Ed25519 VRF keypair for mining.

The output includes:
- Private key (64 bytes hex) - Set as MINER_VRF_KEY environment variable
- Public key (32 bytes hex) - Will be registered on-chain via set-key command

Example:
  aultmined keygen
  export MINER_VRF_KEY="<private-key-hex>"
  aultmined set-key`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Generate new Ed25519 VRF keypair
			privKey, err := ecvrf.GenerateKey(rand.Reader)
			if err != nil {
				return fmt.Errorf("failed to generate VRF key: %w", err)
			}

			pubKey, err := privKey.Public()
			if err != nil {
				return fmt.Errorf("failed to derive public key: %w", err)
			}

			privKeyHex := hex.EncodeToString(privKey.Bytes())
			pubKeyHex := hex.EncodeToString(pubKey.Bytes())

			fmt.Println("========================================")
			fmt.Println("VRF Keypair Generated (Ed25519)")
			fmt.Println("========================================")
			fmt.Println()
			fmt.Println("Private Key (64 bytes, hex):")
			fmt.Println(privKeyHex)
			fmt.Println()
			fmt.Println("Public Key (32 bytes, hex):")
			fmt.Println(pubKeyHex)
			fmt.Println()
			fmt.Println("========================================")
			fmt.Println("Setup Instructions:")
			fmt.Println("========================================")
			fmt.Println()
			fmt.Println("1. Set the private key as environment variable:")
			fmt.Printf("   export MINER_VRF_KEY=\"%s\"\n", privKeyHex)
			fmt.Println()
			fmt.Println("2. Register the public key on-chain:")
			fmt.Println("   aultmined set-key")
			fmt.Println()
			fmt.Println("3. Start mining:")
			fmt.Println("   aultmined mine --yes")
			fmt.Println()
			fmt.Println("IMPORTANT: Keep your private key secure!")

			return nil
		},
	}
}

func setKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-key",
		Short: "Set the owner's VRF key on-chain",
		Long:  `Set or update the VRF key for the owner. Uses MINER_VRF_KEY environment variable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load VRF key from environment
			_, vrfPubKey, err := LoadVRFKey()
			if err != nil {
				return fmt.Errorf("failed to load VRF key: %w", err)
			}

			// Create chain client (uses MINER_OPERATOR_KEY)
			chainClient, err := client.NewChainClient()
			if err != nil {
				return fmt.Errorf("failed to create chain client: %w", err)
			}
			defer chainClient.Close()

			// Get owner address from operator key
			ownerAddr, err := chainClient.GetOwnerAddress()
			if err != nil {
				return fmt.Errorf("failed to get owner address: %w", err)
			}

			// Query current key info
			keyInfo, err := chainClient.GetOwnerKeyInfo(context.Background(), ownerAddr.String())
			if err != nil && !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("failed to query owner key info: %w", err)
			}

			nonce := uint64(0)
			if keyInfo != nil && keyInfo.VrfPubkey != nil {
				// Check if the VRF key is already set to the same value
				if bytes.Equal(keyInfo.VrfPubkey, vrfPubKey) {
					fmt.Printf("VRF key already registered for owner %s\n", ownerAddr.String())
					fmt.Printf("VRF Public Key: %x\n", vrfPubKey)
					fmt.Printf("Nonce: %d\n", keyInfo.Nonce)
					fmt.Println("\nNo action needed - key is already set.")
					return nil
				}
				nonce = keyInfo.Nonce
				fmt.Printf("Current VRF key found with nonce %d, updating to new key...\n", nonce)
			} else {
				fmt.Println("No existing VRF key found, setting initial key...")
			}

			fmt.Printf("Setting VRF key for owner %s\n", ownerAddr.String())
			fmt.Printf("VRF Public Key: %x\n", vrfPubKey)
			fmt.Printf("Nonce: %d\n", nonce)

			fmt.Println("\nBroadcasting transaction to chain...")
			if err := chainClient.SetOwnerVRFKey(context.Background(), vrfPubKey, nonce, true); err != nil {
				return fmt.Errorf("failed to set owner VRF key: %w", err)
			}

			fmt.Println("Owner VRF key set successfully!")
			return nil
		},
	}
}

func mineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mine",
		Short: "Start mining with auto-detected licenses",
		Long:  `Start the mining client. Automatically detects owned licenses and mines using the owner's VRF key.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			continuous, _ := cmd.Flags().GetBool("continuous")

			// Create chain client first
			chainClient, err := client.NewChainClient()
			if err != nil {
				return fmt.Errorf("failed to create chain client: %w", err)
			}
			defer chainClient.Close()

			// Create miner manager
			log.Println("Initializing Ault Miner...")
			manager, err := mining.NewMinerManager(chainClient)
			if err != nil {
				return fmt.Errorf("failed to create miner manager: %w", err)
			}

			// Verify VRF key is registered on chain and matches local key
			ownerAddr := manager.GetOwnerAddr().String()
			keyInfo, err := chainClient.GetOwnerKeyInfo(context.Background(), ownerAddr)
			if err != nil || keyInfo == nil || len(keyInfo.VrfPubkey) == 0 {
				return fmt.Errorf("VRF key not registered on chain. Run: ./aultmined set-key")
			}
			if !bytes.Equal(keyInfo.VrfPubkey, manager.GetVRFPubKey()) {
				return fmt.Errorf("local VRF key does not match chain key. Run: ./aultmined set-key")
			}
			log.Printf("VRF key verified (nonce: %d)", keyInfo.Nonce)

			// Setup signal handling
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			// Start API server
			apiAddr := ":" + config.Get().APIPort

			apiServer := api.New(manager.GetStore(), func() interface{} {
				return manager.GetStats()
			}, chainClient)

			go func() {
				log.Printf("Starting API server on %s", apiAddr)
				if err := apiServer.Start(apiAddr); err != nil {
					log.Printf("API server error: %v", err)
				}
			}()

			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				if err := apiServer.Shutdown(shutdownCtx); err != nil {
					log.Printf("API server shutdown error: %v", err)
				}
			}()

			// Handle shutdown
			go func() {
				<-sigChan
				log.Println("Gracefully shutting down miner...")
				cancel()
			}()

			if continuous {
				log.Printf("Starting continuous mining with %d license(s)...", len(manager.GetLicenses()))
				return manager.Start(ctx)
			} else {
				log.Println("Mining single epoch...")
				epochInfo, err := manager.GetChainClient().GetCurrentEpoch(ctx)
				if err != nil {
					return fmt.Errorf("failed to get epoch: %w", err)
				}
				manager.ProcessEpoch(ctx, epochInfo)
				return nil
			}
		},
	}

	cmd.Flags().Bool("continuous", true, "mine continuously")
	cmd.Flags().Bool("yes", false, "skip confirmation prompts")

	return cmd
}

// LoadVRFKey loads VRF key from MINER_VRF_KEY environment variable
func LoadVRFKey() ([]byte, []byte, error) {
	hexKey := config.Get().VRFKey
	if hexKey == "" {
		return nil, nil, fmt.Errorf("MINER_VRF_KEY environment variable is required")
	}

	privKeyBytes, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hex VRF key: %w", err)
	}

	pk, err := ecvrf.NewPrivateKey(privKeyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid ECVRF private key: %w", err)
	}

	pubKey, err := pk.Public()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to derive public key: %w", err)
	}

	log.Println("Loaded VRF key from MINER_VRF_KEY")
	return pk.Bytes(), pubKey.Bytes(), nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
