package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/big"

	"github.com/ethereum-optimism/optimism/op-node/rollup"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/schollz/progressbar/v3"
)

const DerivationVersionCelestia = 0xce

// NOTE: change to ancient8 for Ancient8 network
const NETWORK = "publicgoods"
const TOKEN = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJwdWJsaWMiLCJyZWFkIiwid3JpdGUiLCJhZG1pbiJdfQ.OzQirSbuEUaFZ9b-iHPCxNmE-SDLcaVdYQY3hxHS_is"

var batcherAddr, batchInboxAddress common.Address
var rpc, namespaceId string
var l2BlockNumber *big.Int

func L1Signer() types.Signer {
	L1ChainID := big.NewInt(1)
	return types.NewCancunSigner(L1ChainID)
}

func main() {
	if NETWORK == "ancient8" {
		batcherAddr = common.HexToAddress("0x6079e9c37b87fE06D0bDe2431a0fa309826c9b67")
		batchInboxAddress = common.HexToAddress("0xd5df46c580fD2FBdaEE751dc535E14295C0336F3")
		rpc = "https://rpc.ancient8.gg"
		namespaceId = "0000000000000000000000000000000000000000000c4e2f56f57246c8"
		l2BlockNumber = big.NewInt(12717114)
	} else if NETWORK == "publicgoods" {
		batcherAddr = common.HexToAddress("0x99526b0e49a95833e734eb556a6abaffab0ee167")
		batchInboxAddress = common.HexToAddress("0xc1b90e1e459abbdcec4dcf90da45ba077d83bfc5")
		rpc = "https://rpc.publicgoods.network/"
		namespaceId = "0000000000000000000000000000000000000000000cdb4471d975b186"
		l2BlockNumber = big.NewInt(12717114)
	} else {
		log.Fatalf("Invalid network: %s", NETWORK)
	}

	l2Client, err := ethclient.Dial(rpc)
	if err != nil {
		log.Fatalf("Failed to connect to L2 network: %v", err)
	}

	l1Client, err := ethclient.Dial("https://eth.llamarpc.com")
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum (L1) network: %v", err)
	}

	l2Block, err := l2Client.BlockByNumber(context.Background(), l2BlockNumber)
	if err != nil {
		log.Panicf("Failed to fetch L2 block %d: %v", l2BlockNumber, err)
	}

	opaqueTxs := make([]hexutil.Bytes, len(l2Block.Transactions()))
	for i, tx := range l2Block.Transactions() {
		data, err := tx.MarshalBinary()
		if err != nil {
			fmt.Errorf("failed to encode tx %d from RPC: %w", i, err)
			return
		}
		opaqueTxs[i] = data
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(opaqueTxs[0]); err != nil {
		fmt.Errorf("failed to decode first tx to read l1 info from: %w", err)
		return
	}
	if tx.Type() != types.DepositTxType {
		fmt.Errorf("first payload tx has unexpected tx type: %d", tx.Type())
		return
	}

	info, err := derive.L1BlockInfoFromBytes(&rollup.Config{}, l2Block.Time(), tx.Data())
	if err != nil {
		ecotoneTime := l2Block.Time() - 1
		info, err = derive.L1BlockInfoFromBytes(&rollup.Config{EcotoneTime: &ecotoneTime}, l2Block.Time(), tx.Data())
		if err != nil {
			log.Panicf("failed to parse L1 info deposit tx from L2 block: %w", err)
			return
		}
	}
	fmt.Println(info.BlockHash.Hex())

	fmt.Printf("L2 Block Number: %d\n", l2Block.Number())
	fmt.Printf("L2 Block Hash: %s\n", l2Block.Hash().Hex())
	fmt.Printf("L2 Block Time: %d\n", l2Block.Time())
	fmt.Println("Number of L2 Transactions: ", len(l2Block.Transactions()))

	latestL1Block, err := l1Client.BlockByNumber(context.Background(), nil)
	if err != nil {
		log.Panicf("Failed to fetch latest Ethereum block: %v", err)
	}

	if latestL1Block.Time() < l2Block.Time() {
		log.Fatalf("L2 block %d is ahead of the latest Ethereum block %d", l2BlockNumber, latestL1Block.Number())
	}

	l1BlockNumber := info.Number

	fmt.Printf("Looking in the next blocks on Ethereum (L1) after block %d to find batches posted to batch inbox address \n", l1BlockNumber)

	bar := progressbar.Default(1200)
	for i := 0; i < 1200; i++ {
		bar.Add(1)

		blockNumber := big.NewInt(int64(l1BlockNumber) + int64(i))
		block, err := l1Client.BlockByNumber(context.Background(), blockNumber)
		if err != nil {
			fmt.Println("Failed to fetch Ethereum block %d: %v", blockNumber, err)
			continue
		}

		transactions := block.Transactions()
		data, err := DataFromEVMTransactions(batcherAddr, transactions)
		if err != nil {
			log.Fatalf("Failed to fetch data from Ethereum transactions: %v", err)
		}

		for _, d := range data {
			frames, err := derive.ParseFrames(d)
			if err != nil {
				log.Fatalf("Celestia: failed to parse frames", "err", err)
				continue
			}
			fmt.Println("frames length:", len(frames))

			for _, frame := range frames {
				fmt.Println("frame data length:", len(frame.Data))
				data, _ := io.ReadAll(io.MultiReader(bytes.NewReader(frame.Data)))
				if f, err := derive.BatchReader(bytes.NewBuffer(data)); err == nil {
					batchData, err := f()
					if err != nil {
						fmt.Println("Celestia: failed to read frame", "err", err)
						continue
					}

					singularBatch, err := derive.GetSingularBatch(batchData)
					if err != nil {
						fmt.Println("Celestia: failed to get singular batch", "err", err)
						continue
					}
					fmt.Println("txs length in the batch", len(singularBatch.Transactions))

				} else {
					fmt.Println("Celestia: failed to create batch reader", "err", err)
				}
			}
		}
	}
}

func isValidBatchTx(tx *types.Transaction, l1Signer types.Signer, batchInboxAddr, batcherAddr common.Address) bool {
	to := tx.To()
	if to == nil || *to != batchInboxAddr {
		return false
	}
	seqDataSubmitter, err := l1Signer.Sender(tx) // optimization: only derive sender if To is correct
	if err != nil {
		fmt.Println("tx in inbox with invalid signature", "hash", tx.Hash(), "err", err)
		return false
	}
	// some random L1 user might have sent a transaction to our batch inbox, ignore them
	if seqDataSubmitter != batcherAddr {
		fmt.Println("tx in inbox with unauthorized submitter", "addr", seqDataSubmitter, "hash", tx.Hash(), "err", err)
		return false
	}
	return true
}

func DataFromEVMTransactions(batcherAddr common.Address, txs types.Transactions) ([]eth.Data, error) {
	daClient, err := NewDAClient("http://127.0.0.1:26658", TOKEN, namespaceId)
	if err != nil {
		log.Panicf("Failed to create DA client: %v", err)
	}

	out := []eth.Data{}
	for _, tx := range txs {
		if isValidBatchTx(tx, L1Signer(), batchInboxAddress, batcherAddr) {
			data := tx.Data()
			switch len(data) {
			case 0:
				out = append(out, data)
			default:
				switch data[0] {
				case DerivationVersionCelestia:
					fmt.Println("\nCelestia: found a celestia starting byte in calldate", "id", hex.EncodeToString(tx.Data()))
					ctx, cancel := context.WithTimeout(context.Background(), daClient.GetTimeout)
					blobs, err := daClient.Client.Get(ctx, [][]byte{data[1:]}, daClient.Namespace)
					cancel()
					if err != nil {
						log.Panicf("Celestia: failed to resolve frame", "err", err)
					}
					if len(blobs) != 1 {
						fmt.Println("Celestia: unexpected length for blobs", "expected", 1, "got", len(blobs))
						if len(blobs) == 0 {
							fmt.Println("Celestia: skipping empty blobs")
							continue
						}
					}

					out = append(out, blobs[0])
				default:
					out = append(out, data)
					fmt.Println("Celestia: using eth fallback")
				}
			}
		}
	}

	return out, nil
}
