package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var erc20ABI = mustABI(`[
  {"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"type":"function"},
  {"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"type":"function"}
]`)

var feedABI = mustABI(`[
  {"inputs":[],"name":"decimals","outputs":[{"type":"uint8"}],"stateMutability":"view","type":"function"},
  {"inputs":[],"name":"latestRoundData","outputs":[
     {"type":"uint80"},{"type":"int256"},{"type":"uint256"},{"type":"uint256"},{"type":"uint80"}
  ],"stateMutability":"view","type":"function"}
]`)

var tokenFeeds = []struct {
	Symbol    string
	TokenAddr common.Address
	FeedAddr  common.Address
	Decimals  int
}{
	{"ETH", common.Address{}, common.HexToAddress("0x5f4ec3df9cbd43714fe2740f5e3616155c5b8419"), 18},
	{"WETH", common.HexToAddress("0xC02aaA39b223FE8D0A0E5C4F27eAD9083C756Cc2"), common.HexToAddress("0x5f4ec3df9cbd43714fe2740f5e3616155c5b8419"), 18},
	{"USDC", common.HexToAddress("0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"), common.HexToAddress("0x8fFfFfd4AfB6115b954Bd326cbe7b4Ba576818f6"), 6},
	{"DAI", common.HexToAddress("0x6B175474E89094C44Da98b954EedeAC495271d0F"), common.HexToAddress("0xAed0c38402a5d19df6E4c03F4E2DceD6e29c1ee9"), 18},
	{"LINK", common.HexToAddress("0x514910771AF9Ca656af840dff83E8264EcF986CA"), common.HexToAddress("0x2c1d072e956AFFC0D435Cb7AC38EF18d24d9127c"), 18},
}

func mustABI(jsonStr string) abi.ABI {
	a, err := abi.JSON(strings.NewReader(jsonStr))
	if err != nil {
		log.Fatalf("ABI parse error: %v", err)
	}
	return a
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <ethereum_address>", os.Args[0])
	}
	wallet := common.HexToAddress(os.Args[1])

	rpc := os.Getenv("ETH_RPC_URL")
	if rpc == "" {
		log.Fatal("Please set ETH_RPC_URL env var")
	}

	client, err := ethclient.Dial(rpc)
	if err != nil {
		log.Fatalf("RPC dial error: %v", err)
	}
	ctx := context.Background()

	totalUSD := big.NewFloat(0)

	for _, tf := range tokenFeeds {
		var balRaw *big.Int
		if tf.Symbol == "ETH" {
			balRaw, err = client.BalanceAt(ctx, wallet, nil)
		} else {
			balRaw, err = erc20Balance(ctx, client, tf.TokenAddr, wallet)
		}
		if err != nil || balRaw.Sign() == 0 {
			continue
		}

		price, err := feedPrice(ctx, client, tf.FeedAddr)
		if err != nil {
			continue
		}

		amt := new(big.Float).Quo(new(big.Float).SetInt(balRaw),
			big.NewFloat(math.Pow10(tf.Decimals)))
		usd := new(big.Float).Mul(amt, price)

		fmt.Printf("%-6s %12s => $%s\n",
			tf.Symbol,
			amt.Text('f', 6),
			usd.Text('f', 2),
		)
		totalUSD.Add(totalUSD, usd)
	}

	fmt.Printf("TOTAL %12s => $%s\n", "",
		totalUSD.Text('f', 2))
}

func feedPrice(ctx context.Context, client *ethclient.Client, feedAddr common.Address) (*big.Float, error) {
	bz, err := feedABI.Pack("decimals")
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &feedAddr, Data: bz}, nil)
	if err != nil {
		return nil, err
	}
	dec := new(big.Int).SetBytes(out)
	bz, err = feedABI.Pack("latestRoundData")
	if err != nil {
		return nil, err
	}
	out2, err := client.CallContract(ctx, ethereum.CallMsg{To: &feedAddr, Data: bz}, nil)
	if err != nil {
		return nil, err
	}
	_, answerRaw, _, _, _ := unpackLatest(out2)
	price := new(big.Float).Quo(
		new(big.Float).SetInt(answerRaw),
		big.NewFloat(math.Pow10(int(dec.Int64()))),
	)
	return price, nil
}

func unpackLatest(data []byte) (roundId *big.Int, answer *big.Int, startedAt, updatedAt, answeredInRound *big.Int) {
	vs, err := feedABI.Unpack("latestRoundData", data)
	if err != nil {
		log.Fatalf("unpack latestRoundData: %v", err)
	}
	return vs[0].(*big.Int), vs[1].(*big.Int), vs[2].(*big.Int), vs[3].(*big.Int), vs[4].(*big.Int)
}

func erc20Balance(ctx context.Context, client *ethclient.Client, tokenAddr, user common.Address) (*big.Int, error) {
	bz, err := erc20ABI.Pack("balanceOf", user)
	if err != nil {
		return nil, err
	}
	out, err := client.CallContract(ctx, ethereum.CallMsg{To: &tokenAddr, Data: bz}, nil)
	if err != nil {
		return nil, err
	}
	vs, err := erc20ABI.Unpack("balanceOf", out)
	if err != nil {
		return nil, err
	}
	return vs[0].(*big.Int), nil
}
