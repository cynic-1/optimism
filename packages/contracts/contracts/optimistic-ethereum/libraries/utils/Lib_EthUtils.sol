// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.7.0;
pragma experimental ABIEncoderV2;

/* Library Imports */
import { Lib_RLPWriter } from "../rlp/Lib_RLPWriter.sol";

library Lib_EthUtils {
    function getCode(
        address _address,
        uint256 _offset,
        uint256 _length
    )
        internal
        view
        returns (
            bytes memory _code
        )
    {
        assembly {
            _code := mload(0x40)
            mstore(0x40, add(_code, add(_length, 0x20)))
            mstore(_code, _length)
            extcodecopy(_address, add(_code, 0x20), _offset, _length)
        }

        return _code;
    }

    function getCode(
        address _address
    )
        internal
        view
        returns (
            bytes memory _code
        )
    {
        return getCode(
            _address,
            0,
            getCodeSize(_address)
        );
    }

    function getCodeSize(
        address _address
    )
        internal
        view
        returns (
            uint256 _codeSize
        )
    {
        assembly {
            _codeSize := extcodesize(_address)
        }

        return _codeSize;
    }

    function getCodeHash(
        address _address
    )
        internal
        view
        returns (
            bytes32 _codeHash
        )
    {
        assembly {
            _codeHash := extcodehash(_address)
        }

        return _codeHash;
    }

    function createContract(
        bytes memory _code
    )
        internal
        returns (
            address _created
        )
    {
        assembly {
            _created := create(
                0,
                add(_code, 0x20),
                mload(_code)
            )
        }

        return _created;
    }

    function getAddressForCREATE(
        address _creator,
        uint256 _nonce
    )
        internal
        view
        returns (
            address _address
        )
    {
        bytes[] memory encoded = new bytes[](2);
        encoded[0] = Lib_RLPWriter.encodeAddress(_creator);
        encoded[1] = Lib_RLPWriter.encodeUint(_nonce);

        bytes memory encodedList = Lib_RLPWriter.encodeList(encoded);
        return getAddressFromHash(keccak256(encodedList));
    }

    function getAddressForCREATE2(
        address _creator,
        bytes memory _bytecode,
        bytes32 _salt
    )
        internal
        view
        returns (address _address)
    {
        bytes32 hashedData = keccak256(abi.encodePacked(
            byte(0xff),
            _creator,
            _salt,
            keccak256(_bytecode)
        ));

        return getAddressFromHash(hashedData);
    }

    /**
     * Determines an address from a 32 byte hash. Since addresses are only
     * 20 bytes, we need to retrieve the last 20 bytes from the original
     * hash. Converting to uint256 and then uint160 gives us these bytes.
     * @param _hash Hash to convert to an address.
     * @return Hash converted to an address.
     */
    function getAddressFromHash(
        bytes32 _hash
    )
        private
        pure
        returns (address)
    {
        return address(bytes20(uint160(uint256(_hash))));
    }
}
