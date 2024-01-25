package datastore

import (
	"fmt"

	"github.com/PretendoNetwork/nex-go"
	"github.com/PretendoNetwork/nex-go/types"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/globals"
	datastore "github.com/PretendoNetwork/nex-protocols-go/datastore"
)

func completePostObjects(err error, packet nex.PacketInterface, callID uint32, dataIDs *types.List[*types.PrimitiveU64]) (*nex.RMCMessage, uint32) {
	if commonProtocol.minIOClient == nil {
		common_globals.Logger.Warning("MinIOClient not defined")
		return nil, nex.ResultCodes.Core.NotImplemented
	}

	if commonProtocol.GetObjectSizeByDataID == nil {
		common_globals.Logger.Warning("GetObjectSizeByDataID not defined")
		return nil, nex.ResultCodes.Core.NotImplemented
	}

	if commonProtocol.UpdateObjectUploadCompletedByDataID == nil {
		common_globals.Logger.Warning("UpdateObjectUploadCompletedByDataID not defined")
		return nil, nex.ResultCodes.Core.NotImplemented
	}

	if err != nil {
		common_globals.Logger.Error(err.Error())
		return nil, nex.ResultCodes.DataStore.Unknown
	}

	// TODO - This assumes a PRUDP connection. Refactor to support HPP
	connection := packet.Sender().(*nex.PRUDPConnection)
	endpoint := connection.Endpoint
	server := endpoint.Server

	var errorCode uint32

	dataIDs.Each(func(_ int, dataID *types.PrimitiveU64) bool {
		bucket := commonProtocol.S3Bucket
		key := fmt.Sprintf("%s/%d.bin", commonProtocol.s3DataKeyBase, dataID)

		objectSizeS3, err := commonProtocol.S3ObjectSize(bucket, key)
		if err != nil {
			common_globals.Logger.Error(err.Error())
			errorCode = nex.ResultCodes.DataStore.NotFound

			return true
		}

		objectSizeDB, errCode := commonProtocol.GetObjectSizeByDataID(dataID)
		if errCode != 0 {
			errorCode = errCode

			return true
		}

		if objectSizeS3 != uint64(objectSizeDB) {
			common_globals.Logger.Errorf("Object with DataID %d did not upload correctly! Mismatched sizes", dataID)
			// TODO - Is this a good error?
			errorCode = nex.ResultCodes.DataStore.Unknown

			return true
		}

		errCode = commonProtocol.UpdateObjectUploadCompletedByDataID(dataID, true)
		if errCode != 0 {
			errorCode = errCode

			return true
		}

		return false
	})

	if errorCode != 0 {
		return nil, errorCode
	}

	rmcResponse := nex.NewRMCSuccess(server, nil)
	rmcResponse.ProtocolID = datastore.ProtocolID
	rmcResponse.MethodID = datastore.MethodCompletePostObjects
	rmcResponse.CallID = callID

	return rmcResponse, 0
}
