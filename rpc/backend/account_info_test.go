package backend

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"google.golang.org/grpc/metadata"

	"github.com/cometbft/cometbft/libs/bytes"
	cmtrpcclient "github.com/cometbft/cometbft/rpc/client"

	"github.com/cosmos/evm/rpc/backend/mocks"
	rpctypes "github.com/cosmos/evm/rpc/types"
	utiltx "github.com/cosmos/evm/testutil/tx"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

func (suite *BackendTestSuite) TestGetCode() {
	blockNr := rpctypes.NewBlockNumber(big.NewInt(1))
	contractCode := []byte("0xef616c92f3cfc9e92dc270d6acff9cea213cecc7020a76ee4395af09bdceb4837a1ebdb5735e11e7d3adb6104e0c3ac55180b4ddf5e54d022cc5e8837f6a4f971b")

	testCases := []struct {
		name          string
		addr          common.Address
		blockNrOrHash rpctypes.BlockNumberOrHash
		registerMock  func(common.Address)
		expPass       bool
		expCode       hexutil.Bytes
	}{
		{
			"fail - BlockHash and BlockNumber are both nil ",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{},
			func(_ common.Address) {},
			false,
			nil,
		},
		{
			"fail - query client errors on getting Code",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(addr common.Address) {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterCodeError(queryClient, addr)
			},
			false,
			nil,
		},
		{
			"pass",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(addr common.Address) {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterCode(queryClient, addr, contractCode)
			},
			true,
			contractCode,
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset
			tc.registerMock(tc.addr)

			code, err := suite.backend.GetCode(tc.addr, tc.blockNrOrHash)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expCode, code)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *BackendTestSuite) TestGetProof() {
	blockNrInvalid := rpctypes.NewBlockNumber(big.NewInt(1))
	blockNr := rpctypes.NewBlockNumber(big.NewInt(4))
	blockNrZero := rpctypes.NewBlockNumber(big.NewInt(0))
	address1 := utiltx.GenerateAddress()

	testCases := []struct {
		name          string
		addr          common.Address
		storageKeys   []string
		blockNrOrHash rpctypes.BlockNumberOrHash
		registerMock  func(rpctypes.BlockNumber, common.Address)
		expPass       bool
		expAccRes     *rpctypes.AccountResult
	}{
		{
			"fail - BlockNumeber = 1 (invalidBlockNumber)",
			address1,
			[]string{},
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNrInvalid},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterAccount(queryClient, addr, blockNrInvalid.Int64())
			},
			false,
			&rpctypes.AccountResult{},
		},
		{
			"fail - Block doesn't exist",
			address1,
			[]string{},
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNrInvalid},
			func(bn rpctypes.BlockNumber, _ common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				RegisterBlockError(client, bn.Int64())
			},
			false,
			&rpctypes.AccountResult{},
		},
		{
			"pass",
			address1,
			[]string{"0x0"},
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				suite.backend.ctx = rpctypes.ContextWithHeight(bn.Int64())

				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterAccount(queryClient, addr, bn.Int64())

				// Use the IAVL height if a valid tendermint height is passed in.
				iavlHeight := bn.Int64()
				RegisterABCIQueryWithOptions(
					client,
					bn.Int64(),
					"store/evm/key",
					evmtypes.StateKey(address1, common.HexToHash("0x0").Bytes()),
					cmtrpcclient.ABCIQueryOptions{Height: iavlHeight, Prove: true},
				)
				RegisterABCIQueryWithOptions(
					client,
					bn.Int64(),
					"store/acc/key",
					bytes.HexBytes(append(authtypes.AddressStoreKeyPrefix, address1.Bytes()...)),
					cmtrpcclient.ABCIQueryOptions{Height: iavlHeight, Prove: true},
				)
			},
			true,
			&rpctypes.AccountResult{
				Address:      address1,
				AccountProof: []string{""},
				Balance:      (*hexutil.Big)(big.NewInt(0)),
				CodeHash:     common.HexToHash(""),
				Nonce:        0x0,
				StorageHash:  common.Hash{},
				StorageProof: []rpctypes.StorageResult{
					{
						Key:   "0x0",
						Value: (*hexutil.Big)(big.NewInt(2)),
						Proof: []string{""},
					},
				},
			},
		},
		{
			"pass, 0 height",
			address1,
			[]string{"0x0"},
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNrZero},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				suite.backend.ctx = rpctypes.ContextWithHeight(4)

				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, 4, nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterAccount(queryClient, addr, 4)
				var header metadata.MD
				RegisterParams(queryClient, &header, 4)

				// Use the IAVL height if a valid tendermint height is passed in.
				var iavlHeight int64 = 4
				RegisterABCIQueryWithOptions(
					client,
					iavlHeight,
					"store/evm/key",
					evmtypes.StateKey(address1, common.HexToHash("0x0").Bytes()),
					cmtrpcclient.ABCIQueryOptions{Height: iavlHeight, Prove: true},
				)
				RegisterABCIQueryWithOptions(
					client,
					iavlHeight,
					"store/acc/key",
					bytes.HexBytes(append(authtypes.AddressStoreKeyPrefix, address1.Bytes()...)),
					cmtrpcclient.ABCIQueryOptions{Height: iavlHeight, Prove: true},
				)
			},
			true,
			&rpctypes.AccountResult{
				Address:      address1,
				AccountProof: []string{""},
				Balance:      (*hexutil.Big)(big.NewInt(0)),
				CodeHash:     common.HexToHash(""),
				Nonce:        0x0,
				StorageHash:  common.Hash{},
				StorageProof: []rpctypes.StorageResult{
					{
						Key:   "0x0",
						Value: (*hexutil.Big)(big.NewInt(2)),
						Proof: []string{""},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest()
			tc.registerMock(*tc.blockNrOrHash.BlockNumber, tc.addr)

			accRes, err := suite.backend.GetProof(tc.addr, tc.storageKeys, tc.blockNrOrHash)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expAccRes, accRes)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *BackendTestSuite) TestGetStorageAt() {
	blockNr := rpctypes.NewBlockNumber(big.NewInt(1))

	testCases := []struct {
		name          string
		addr          common.Address
		key           string
		blockNrOrHash rpctypes.BlockNumberOrHash
		registerMock  func(common.Address, string, string)
		expPass       bool
		expStorage    hexutil.Bytes
	}{
		{
			"fail - BlockHash and BlockNumber are both nil",
			utiltx.GenerateAddress(),
			"0x0",
			rpctypes.BlockNumberOrHash{},
			func(common.Address, string, string) {},
			false,
			nil,
		},
		{
			"fail - query client errors on getting Storage",
			utiltx.GenerateAddress(),
			"0x0",
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(addr common.Address, key string, _ string) {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterStorageAtError(queryClient, addr, key)
			},
			false,
			nil,
		},
		{
			"pass",
			utiltx.GenerateAddress(),
			"0x0",
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(addr common.Address, key string, storage string) {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterStorageAt(queryClient, addr, key, storage)
			},
			true,
			hexutil.Bytes{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0},
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest()
			tc.registerMock(tc.addr, tc.key, tc.expStorage.String())

			storage, err := suite.backend.GetStorageAt(tc.addr, tc.key, tc.blockNrOrHash)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expStorage, storage)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *BackendTestSuite) TestGetBalance() {
	blockNr := rpctypes.NewBlockNumber(big.NewInt(1))

	testCases := []struct {
		name          string
		addr          common.Address
		blockNrOrHash rpctypes.BlockNumberOrHash
		registerMock  func(rpctypes.BlockNumber, common.Address)
		expPass       bool
		expBalance    *hexutil.Big
	}{
		{
			"fail - BlockHash and BlockNumber are both nil",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{},
			func(rpctypes.BlockNumber, common.Address) {
			},
			false,
			nil,
		},
		{
			"fail - tendermint client failed to get block",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, _ common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				RegisterBlockError(client, bn.Int64())
			},
			false,
			nil,
		},
		{
			"fail - query client failed to get balance",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBalanceError(queryClient, addr, bn.Int64())
			},
			false,
			nil,
		},
		{
			"fail - invalid balance",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBalanceInvalid(queryClient, addr, bn.Int64())
			},
			false,
			nil,
		},
		{
			"fail - pruned node state",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBalanceNegative(queryClient, addr, bn.Int64())
			},
			false,
			nil,
		},
		{
			"pass",
			utiltx.GenerateAddress(),
			rpctypes.BlockNumberOrHash{BlockNumber: &blockNr},
			func(bn rpctypes.BlockNumber, addr common.Address) {
				client := suite.backend.clientCtx.Client.(*mocks.Client)
				_, err := RegisterBlock(client, bn.Int64(), nil)
				suite.Require().NoError(err)
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBalance(queryClient, addr, bn.Int64())
			},
			true,
			(*hexutil.Big)(big.NewInt(1)),
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest()

			// avoid nil pointer reference
			if tc.blockNrOrHash.BlockNumber != nil {
				tc.registerMock(*tc.blockNrOrHash.BlockNumber, tc.addr)
			}

			balance, err := suite.backend.GetBalance(tc.addr, tc.blockNrOrHash)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expBalance, balance)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *BackendTestSuite) TestGetTransactionCount() {
	testCases := []struct {
		name         string
		accExists    bool
		blockNum     rpctypes.BlockNumber
		registerMock func(common.Address, rpctypes.BlockNumber)
		expPass      bool
		expTxCount   hexutil.Uint64
	}{
		{
			"pass - account doesn't exist",
			false,
			rpctypes.NewBlockNumber(big.NewInt(1)),
			func(common.Address, rpctypes.BlockNumber) {
				var header metadata.MD
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterParams(queryClient, &header, 1)
			},
			true,
			hexutil.Uint64(0),
		},
		{
			"fail - block height is in the future",
			false,
			rpctypes.NewBlockNumber(big.NewInt(10000)),
			func(common.Address, rpctypes.BlockNumber) {
				var header metadata.MD
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterParams(queryClient, &header, 1)
			},
			false,
			hexutil.Uint64(0),
		},
		// TODO: Error mocking the GetAccount call - problem with Any type
		// {
		//	"pass - returns the number of transactions at the given address up to the given block number",
		//	true,
		//	rpctypes.NewBlockNumber(big.NewInt(1)),
		//	func(addr common.Address, bn rpctypes.BlockNumber) {
		//		client := suite.backend.clientCtx.Client.(*mocks.Client)
		//		account, err := suite.backend.clientCtx.AccountRetriever.GetAccount(suite.backend.clientCtx, suite.acc)
		//		suite.Require().NoError(err)
		//		request := &authtypes.QueryAccountRequest{Address: sdk.AccAddress(suite.acc.Bytes()).String()}
		//		requestMarshal, _ := request.Marshal()
		//		RegisterABCIQueryAccount(
		//			client,
		//			requestMarshal,
		//			cmtrpcclient.ABCIQueryOptions{Height: int64(1), Prove: false},
		//			account,
		//		)
		//	},
		//	true,
		//	hexutil.Uint64(0),
		// },
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest()

			addr := utiltx.GenerateAddress()
			if tc.accExists {
				addr = common.BytesToAddress(suite.acc.Bytes())
			}

			tc.registerMock(addr, tc.blockNum)

			txCount, err := suite.backend.GetTransactionCount(addr, tc.blockNum)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expTxCount, *txCount)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
