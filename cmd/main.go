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
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/evm/crypto/ethsecp256k1"

	"github.com/Ault-Blockchain/ault-miner-node/api"
	"github.com/Ault-Blockchain/ault-miner-node/internal/config"
	"github.com/Ault-Blockchain/ault-miner-node/internal/storage"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/client"
	"github.com/Ault-Blockchain/ault-miner-node/pkg/mining"
	appcfg "github.com/Ault-Blockchain/ault/app/config"
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
		keygenCmd(),
		vrfKeygenCmd(),
		setKeyCmd(),
		mineCmd(),
	)
}

func keygenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keygen",
		Short: "Generate a new operator wallet keypair",
		Long: `Generate a new secp256k1 keypair for the operator wallet.

The output includes:
- Private key (32 bytes hex) - Set as MINER_OPERATOR_KEY environment variable
- Bech32 address - Use for delegation and funding
- EVM address (0x...) - For EVM-compatible tools

Example:
  aultmined keygen
  export MINER_OPERATOR_KEY="<private-key-hex>"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			privKey, err := ethsecp256k1.GenerateKey()
			if err != nil {
				return fmt.Errorf("failed to generate key: %w", err)
			}
			pubKey := privKey.PubKey()
			address := sdk.AccAddress(pubKey.Address())
			evmAddr := common.BytesToAddress([]byte(pubKey.Address()))

			privKeyHex := hex.EncodeToString(privKey.Bytes())

			fmt.Println("========================================")
			fmt.Println("Operator Wallet Generated (secp256k1)")
			fmt.Println("========================================")
			fmt.Println()
			fmt.Println("Private Key (32 bytes, hex):")
			fmt.Println(privKeyHex)
			fmt.Println()
			fmt.Println("Bech32 Address:")
			fmt.Println(address.String())
			fmt.Println()
			fmt.Println("EVM Address:")
			fmt.Println(evmAddr.Hex())
			fmt.Println()
			fmt.Println("========================================")
			fmt.Println("Setup Instructions:")
			fmt.Println("========================================")
			fmt.Println()
			fmt.Println("1. Set the private key as environment variable:")
			fmt.Printf("   export MINER_OPERATOR_KEY=\"%s\"\n", privKeyHex)
			fmt.Println()
			fmt.Println("2. Delegate your licenses to this address")
			fmt.Println(address.String())
			fmt.Println()
			fmt.Println("IMPORTANT: Keep your private key secure!")

			return nil
		},
	}
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
  aultmined vrfkeygen
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

// getVRFPubKeyHex derives the VRF public key hex from the private key hex.
func getVRFPubKeyHex(vrfKeyHex string) string {
	vrfPrivBytes, err := hex.DecodeString(vrfKeyHex)
	if err != nil {
		log.Printf("Warning: failed to decode VRF key: %v", err)
		return ""
	}
	vrfPrivKey, err := ecvrf.NewPrivateKey(vrfPrivBytes)
	if err != nil {
		log.Printf("Warning: failed to create VRF private key: %v", err)
		return ""
	}
	vrfPubKey, err := vrfPrivKey.Public()
	if err != nil {
		log.Printf("Warning: failed to derive VRF public key: %v", err)
		return ""
	}
	return hex.EncodeToString(vrfPubKey.Bytes())
}

func mineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mine",
		Short: "Start mining with auto-detected licenses",
		Long:  `Start the mining client. Automatically detects owned licenses and mines using the owner's VRF key.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			continuous, _ := cmd.Flags().GetBool("continuous")
			cfg := config.Get()

			var chainClient *client.ChainClient
			var operatorKeyHex, vrfKeyHex string
			var ownerAddr sdk.AccAddress
			var err error

			// AUTO MODE: Load or generate keys from storage
			if cfg.AutoMode {
				log.Println("Auto mode enabled: checking for stored keys...")

				// Try to load existing keys from volume
				stored, err := storage.LoadKeys(cfg.DataDir)
				if err != nil {
					return fmt.Errorf("failed to check stored keys: %w", err)
				}

				if stored != nil {
					log.Println("Loaded existing keys from storage")
					operatorKeyHex = stored.OperatorKeyHex
					vrfKeyHex = stored.VRFKeyHex
					ownerAddr, err = storage.DeriveAddressFromKey(operatorKeyHex)
					if err != nil {
						return fmt.Errorf("failed to derive address from stored key: %w", err)
					}
				} else {
					// Generate new keys
					log.Println("Generating new operator wallet...")
					opKey, addr, err := storage.GenerateOperatorKey()
					if err != nil {
						return fmt.Errorf("failed to generate operator key: %w", err)
					}

					log.Println("Generating new VRF key...")
					vrfPriv, _, err := storage.GenerateVRFKey()
					if err != nil {
						return fmt.Errorf("failed to generate VRF key: %w", err)
					}

					// Save to volume
					stored = &storage.StoredKeys{
						OperatorKeyHex: opKey,
						VRFKeyHex:      vrfPriv,
						CreatedAt:      time.Now(),
					}
					if err := storage.SaveKeys(cfg.DataDir, stored); err != nil {
						return fmt.Errorf("failed to save keys: %w", err)
					}

					operatorKeyHex = opKey
					vrfKeyHex = vrfPriv
					ownerAddr = addr
					log.Printf("Keys generated and saved. Operator address: %s", addr.String())
				}

				// Create chain client with auto-generated keys
				chainClient, err = client.NewChainClientWithKey(operatorKeyHex)
				if err != nil {
					return fmt.Errorf("failed to create chain client: %w", err)
				}

				// Store VRF key in environment for MinerManager to pick up
				os.Setenv(config.EnvVRFKey, vrfKeyHex)

			} else {
				// NORMAL MODE: Use keys from environment
				chainClient, err = client.NewChainClient()
				if err != nil {
					return fmt.Errorf("failed to create chain client: %w", err)
				}
				ownerAddr, _ = chainClient.GetOwnerAddress()
			}
			defer chainClient.Close()

			// Setup signal handling
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			// Handle shutdown
			go func() {
				<-sigChan
				log.Println("Gracefully shutting down miner...")
				cancel()
			}()

			// Start API server FIRST (for health checks)
			apiAddr := ":" + cfg.APIPort

			// Create a minimal API server that works before manager is ready.
			apiServer := api.New()

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

			// AUTO MODE: Wait for delegation, register VRF, then mine
			if cfg.AutoMode {
				// Get VRF public key for registration checks.
				vrfPubKeyHex := getVRFPubKeyHex(vrfKeyHex)

				log.Println("Waiting for license delegation...")
				log.Printf("Delegate licenses to: %s", ownerAddr.String())

				// Background loop: wait for delegation -> register VRF -> mine
				vrfRegistered := false
				for {
					select {
					case <-ctx.Done():
						return nil
					default:
					}

					// Check for delegated licenses
					licenses, err := chainClient.GetDelegatedLicenses(ctx, ownerAddr.String())
					if err != nil {
						log.Printf("Error checking licenses: %v", err)
						time.Sleep(30 * time.Second)
						continue
					}

					if len(licenses) == 0 {
						time.Sleep(30 * time.Second)
						continue
					}

					log.Printf("Found %d delegated license(s)!", len(licenses))

					// Check if VRF already registered and matches local key
					if !vrfRegistered {
						// Decode and validate local VRF public key first
						vrfPubBytes, err := hex.DecodeString(vrfPubKeyHex)
						if err != nil || len(vrfPubBytes) != 32 {
							log.Printf("Invalid local VRF key (len=%d) - cannot proceed", len(vrfPubBytes))
							time.Sleep(30 * time.Second)
							continue
						}

						keyInfo, _ := chainClient.GetOwnerKeyInfo(ctx, ownerAddr.String())
						if keyInfo != nil && len(keyInfo.VrfPubkey) == 32 {
							// On-chain key exists - check if it matches local key
							if bytes.Equal(keyInfo.VrfPubkey, vrfPubBytes) {
								log.Println("VRF key already registered and matches local key")
								vrfRegistered = true
							} else {
								// Key mismatch - rotate to local key
								log.Printf("On-chain VRF key differs from local key (nonce %d), rotating...", keyInfo.Nonce)
								if err := chainClient.SetOwnerVRFKey(ctx, vrfPubBytes, keyInfo.Nonce, true); err != nil {
									log.Printf("VRF key rotation failed (will retry): %v", err)
									time.Sleep(30 * time.Second)
									continue
								}
								log.Println("VRF key rotated successfully!")
								vrfRegistered = true
							}
						} else {
							// No key on chain - register new
							log.Println("Registering VRF key on-chain...")
							if err := chainClient.SetOwnerVRFKey(ctx, vrfPubBytes, 0, true); err != nil {
								log.Printf("VRF registration failed (will retry): %v", err)
								time.Sleep(30 * time.Second)
								continue
							}
							log.Println("VRF key registered successfully!")
							vrfRegistered = true
						}
					}

					// VRF registered + licenses exist = ready to mine
					// Create miner manager and start mining
					log.Println("Initializing miner...")
					manager, err := mining.NewMinerManager(chainClient)
					if err != nil {
						log.Printf("Failed to create miner manager: %v (will retry)", err)
						time.Sleep(30 * time.Second)
						continue
					}

					if continuous {
						log.Printf("Starting continuous mining with %d license(s)...", len(licenses))
						return manager.Start(ctx)
					} else {
						log.Println("Mining single epoch...")
						epochInfo, err := chainClient.GetCurrentEpoch(ctx)
						if err != nil {
							return fmt.Errorf("failed to get epoch: %w", err)
						}
						manager.ProcessEpoch(ctx, epochInfo)
						return nil
					}
				}
			}

			// NORMAL MODE: Original flow
			log.Println("Initializing Ault Miner...")
			manager, err := mining.NewMinerManager(chainClient)
			if err != nil {
				return fmt.Errorf("failed to create miner manager: %w", err)
			}

			// Verify VRF key is registered on chain and matches local key
			keyInfo, err := chainClient.GetOwnerKeyInfo(context.Background(), ownerAddr.String())
			if err != nil || keyInfo == nil || len(keyInfo.VrfPubkey) == 0 {
				return fmt.Errorf("VRF key not registered on chain. Run: ./aultmined set-key")
			}
			if !bytes.Equal(keyInfo.VrfPubkey, manager.GetVRFPubKey()) {
				return fmt.Errorf("local VRF key does not match chain key. Run: ./aultmined set-key")
			}
			log.Printf("VRF key verified (nonce: %d)", keyInfo.Nonce)

			if continuous {
				log.Printf("Starting continuous mining with %d license(s)...", len(manager.GetLicenses()))
				return manager.Start(ctx)
			} else {
				log.Println("Mining single epoch...")
				epochInfo, err := chainClient.GetCurrentEpoch(ctx)
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
