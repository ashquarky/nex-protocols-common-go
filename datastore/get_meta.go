package datastore

import (
	"github.com/PretendoNetwork/nex-go"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/globals"
	datastore "github.com/PretendoNetwork/nex-protocols-go/datastore"
	datastore_types "github.com/PretendoNetwork/nex-protocols-go/datastore/types"
)

func getMeta(err error, packet nex.PacketInterface, callID uint32, param *datastore_types.DataStoreGetMetaParam) (*nex.RMCMessage, uint32) {
	if commonProtocol.GetObjectInfoByPersistenceTargetWithPassword == nil {
		common_globals.Logger.Warning("GetObjectInfoByPersistenceTargetWithPassword not defined")
		return nil, nex.ResultCodesCore.NotImplemented
	}

	if commonProtocol.GetObjectInfoByDataIDWithPassword == nil {
		common_globals.Logger.Warning("GetObjectInfoByDataIDWithPassword not defined")
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

	var pMetaInfo *datastore_types.DataStoreMetaInfo
	var errCode uint32

	// * Real server ignores PersistenceTarget if DataID is set
	if param.DataID.Value == 0 {
		pMetaInfo, errCode = commonProtocol.GetObjectInfoByPersistenceTargetWithPassword(param.PersistenceTarget, param.AccessPassword)
	} else {
		pMetaInfo, errCode = commonProtocol.GetObjectInfoByDataIDWithPassword(param.DataID, param.AccessPassword)
	}

	if errCode != 0 {
		return nil, errCode
	}

	errCode = commonProtocol.VerifyObjectPermission(pMetaInfo.OwnerID, connection.PID(), pMetaInfo.Permission)
	if errCode != 0 {
		return nil, errCode
	}

	pMetaInfo.FilterPropertiesByResultOption(param.ResultOption)

	rmcResponseStream := nex.NewByteStreamOut(server)

	pMetaInfo.WriteTo(rmcResponseStream)

	rmcResponseBody := rmcResponseStream.Bytes()

	rmcResponse := nex.NewRMCSuccess(server, rmcResponseBody)
	rmcResponse.ProtocolID = datastore.ProtocolID
	rmcResponse.MethodID = datastore.MethodGetMeta
	rmcResponse.CallID = callID

	return rmcResponse, 0
}
