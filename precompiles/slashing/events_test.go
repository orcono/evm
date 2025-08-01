package slashing_test

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	cmn "github.com/cosmos/evm/precompiles/common"
	"github.com/cosmos/evm/precompiles/slashing"
	"github.com/cosmos/evm/x/vm/statedb"

	storetypes "cosmossdk.io/store/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (s *PrecompileTestSuite) TestUnjailEvent() {
	var (
		stateDB *statedb.StateDB
		ctx     sdk.Context
		method  = s.precompile.Methods[slashing.UnjailMethod]
	)

	testCases := []struct {
		name        string
		malleate    func() []interface{}
		postCheck   func()
		gas         uint64
		expError    bool
		errContains string
	}{
		{
			"success - the correct event is emitted",
			func() []interface{} {
				validator, err := s.network.App.StakingKeeper.GetValidator(ctx, sdk.ValAddress(s.keyring.GetAccAddr(0)))
				s.Require().NoError(err)

				consAddr, err := validator.GetConsAddr()
				s.Require().NoError(err)

				err = s.network.App.SlashingKeeper.Jail(
					s.network.GetContext(),
					consAddr,
				)
				s.Require().NoError(err)

				return []interface{}{
					s.keyring.GetAddr(0),
				}
			},
			func() {
				log := stateDB.Logs()[0]
				s.Require().Equal(log.Address, s.precompile.Address())

				// Check event signature matches the one emitted
				event := s.precompile.Events[slashing.EventTypeValidatorUnjailed]
				s.Require().Equal(crypto.Keccak256Hash([]byte(event.Sig)), common.HexToHash(log.Topics[0].Hex()))
				s.Require().Equal(log.BlockNumber, uint64(ctx.BlockHeight())) //nolint:gosec // G115

				// Check the validator address in the event matches
				hash, err := cmn.MakeTopic(s.keyring.GetAddr(0))
				s.Require().NoError(err)

				s.Require().Equal(hash, log.Topics[1])

				// Check the fully unpacked event matches the one emitted
				var unjailEvent slashing.EventValidatorUnjailed
				err = cmn.UnpackLog(s.precompile.ABI, &unjailEvent, slashing.EventTypeValidatorUnjailed, *log)
				s.Require().NoError(err)
				s.Require().Equal(s.keyring.GetAddr(0), unjailEvent.Validator)
			},
			20000,
			false,
			"",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			s.SetupTest()
			stateDB = s.network.GetStateDB()
			ctx = s.network.GetContext()

			contract := vm.NewContract(s.keyring.GetAddr(0), s.precompile.Address(), uint256.NewInt(0), tc.gas, nil)
			ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			initialGas := ctx.GasMeter().GasConsumed()
			s.Require().Zero(initialGas)

			_, err := s.precompile.Unjail(ctx, &method, stateDB, contract, tc.malleate())

			if tc.expError {
				s.Require().Error(err)
				s.Require().Contains(err.Error(), tc.errContains)
			} else {
				s.Require().NoError(err)
				tc.postCheck()
			}
		})
	}
}
