package ethermanv2

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	mockbridge "github.com/hermeznetwork/hermez-bridge/test/mocksmartcontracts/bridge"
	"github.com/hermeznetwork/hermez-core/ethermanv2/smartcontracts/bridge"
	"github.com/hermeznetwork/hermez-core/ethermanv2/smartcontracts/globalexitrootmanager"
	"github.com/hermeznetwork/hermez-core/ethermanv2/smartcontracts/matic"
	"github.com/hermeznetwork/hermez-core/ethermanv2/smartcontracts/mockverifier"
	"github.com/hermeznetwork/hermez-core/ethermanv2/smartcontracts/proofofefficiency"
)

// NewSimulatedEtherman creates an etherman that uses a simulated blockchain. It's important to notice that the ChainID of the auth
// must be 1337. The address that holds the auth will have an initial balance of 10 ETH
func NewSimulatedEtherman(cfg Config, auth *bind.TransactOpts) (etherman *Client, ethBackend *backends.SimulatedBackend, maticAddr common.Address, mockBridge *mockbridge.Bridge, err error) {
	// 10000000 ETH in wei
	balance, _ := new(big.Int).SetString("10000000000000000000000000", 10) //nolint:gomnd
	address := auth.From
	genesisAlloc := map[common.Address]core.GenesisAccount{
		address: {
			Balance: balance,
		},
	}
	blockGasLimit := uint64(999999999999999999) //nolint:gomnd
	client := backends.NewSimulatedBackend(genesisAlloc, blockGasLimit)

	// Deploy contracts
	const maticDecimalPlaces = 18
	totalSupply, _ := new(big.Int).SetString("10000000000000000000000000000", 10) //nolint:gomnd
	maticAddr, _, maticContract, err := matic.DeployMatic(auth, client, "Matic Token", "MATIC", maticDecimalPlaces, totalSupply)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	rollupVerifierAddr, _, _, err := mockverifier.DeployMockverifier(auth, client)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	nonce, err := client.PendingNonceAt(context.TODO(), auth.From)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	calculatedBridgeAddr := crypto.CreateAddress(auth.From, nonce+1)
	const pos = 2
	calculatedPoEAddr := crypto.CreateAddress(auth.From, nonce+pos)
	var genesis [32]byte
	exitManagerAddr, _, globalExitRoot, err := globalexitrootmanager.DeployGlobalexitrootmanager(auth, client, calculatedPoEAddr, calculatedBridgeAddr)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	bridgeAddr, _, mockbr, err := mockbridge.DeployBridge(auth, client, 0, exitManagerAddr)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	br, err := bridge.NewBridge(bridgeAddr, client)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	poeAddr, _, poe, err := proofofefficiency.DeployProofofefficiency(auth, client, exitManagerAddr, maticAddr, rollupVerifierAddr, genesis, auth.From, true)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}

	if calculatedBridgeAddr != bridgeAddr {
		return nil, nil, common.Address{}, nil, fmt.Errorf("bridgeAddr (%s) is different from the expected contract address (%s)",
			bridgeAddr.String(), calculatedBridgeAddr.String())
	}
	if calculatedPoEAddr != poeAddr {
		return nil, nil, common.Address{}, nil, fmt.Errorf("poeAddr (%s) is different from the expected contract address (%s)",
			poeAddr.String(), calculatedPoEAddr.String())
	}

	// Approve the bridge and poe to spend 10000 matic tokens.
	approvedAmount, _ := new(big.Int).SetString("10000000000000000000000", 10) //nolint:gomnd
	_, err = maticContract.Approve(auth, bridgeAddr, approvedAmount)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}
	_, err = maticContract.Approve(auth, poeAddr, approvedAmount)
	if err != nil {
		return nil, nil, common.Address{}, nil, err
	}

	client.Commit()
	return &Client{EtherClient: client, PoE: poe, Bridge: br, GlobalExitRootManager: globalExitRoot, SCAddresses: []common.Address{poeAddr, exitManagerAddr, bridgeAddr}, auth: auth}, client, maticAddr, mockbr, nil
}