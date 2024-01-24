package datastore

import (
	"github.com/PretendoNetwork/nex-go"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/globals"
	datastore "github.com/PretendoNetwork/nex-protocols-go/datastore"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/datastore/types"
)

func changeMeta(err error, packet nex.PacketInterface, callID uint32, param *datastore_types.DataStoreChangeMetaParam) (*nex.RMCMessage, uint32) {
	if commonProtocol.GetObjectInfoByDataID == nil {
		common_globals.Logger.Warning("GetObjectInfoByDataID not defined")
		return nil, nex.ResultCodesCore.NotImplemented
	}

	if commonProtocol.UpdateObjectPeriodByDataIDWithPassword == nil {
		common_globals.Logger.Warning("UpdateObjectPeriodByDataIDWithPassword not defined")
		return nil, nex.ResultCodesCore.NotImplemented
	}

	if commonProtocol.UpdateObjectMetaBinaryByDataIDWithPassword == nil {
		common_globals.Logger.Warning("UpdateObjectMetaBinaryByDataIDWithPassword not defined")
		return nil, nex.ResultCodesCore.NotImplemented
	}

	if commonProtocol.UpdateObjectDataTypeByDataIDWithPassword == nil {
		common_globals.Logger.Warning("UpdateObjectDataTypeByDataIDWithPassword not defined")
		return nil, nex.ResultCodesCore.NotImplemented
	}

	if err != nil {
		common_globals.Logger.Error(err.Error())
		return nil, nex.ResultCodesDataStore.Unknown
	}

	// TODO - This assumes a PRUDP connection. Refactor to support HPP
	connection := packet.Sender().(*nex.PRUDPConnection)
	endpoint := connection.Endpoint
	server := endpoint.Server

	metaInfo, errCode := commonProtocol.GetObjectInfoByDataID(param.DataID)
	if errCode != 0 {
		return nil, errCode
	}

	// TODO - Is this the right permission?
	errCode = commonProtocol.VerifyObjectPermission(metaInfo.OwnerID, connection.PID(), metaInfo.DelPermission)
	if errCode != 0 {
		return nil, errCode
	}

	if param.ModifiesFlag.PAND(0x08) != 0 {
		errCode = commonProtocol.UpdateObjectPeriodByDataIDWithPassword(param.DataID, param.Period, param.UpdatePassword)
		if errCode != 0 {
			return nil, errCode
		}
	}

	if param.ModifiesFlag.PAND(0x10) != 0 {
		errCode = commonProtocol.UpdateObjectMetaBinaryByDataIDWithPassword(param.DataID, param.MetaBinary, param.UpdatePassword)
		if errCode != 0 {
			return nil, errCode
		}
	}

	if param.ModifiesFlag.PAND(0x80) != 0 {
		errCode = commonProtocol.UpdateObjectDataTypeByDataIDWithPassword(param.DataID, param.DataType, param.UpdatePassword)
		if errCode != 0 {
			return nil, errCode
		}
	}

	rmcResponse := nex.NewRMCSuccess(server, nil)
	rmcResponse.ProtocolID = datastore.ProtocolID
	rmcResponse.MethodID = datastore.MethodChangeMeta
	rmcResponse.CallID = callID

	return rmcResponse, 0
}
