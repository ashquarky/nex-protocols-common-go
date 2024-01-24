package secureconnection

import (
	"net"
	"strconv"

	"github.com/PretendoNetwork/nex-go"
	"github.com/PretendoNetwork/nex-go/types"
	secure_connection "github.com/PretendoNetwork/nex-protocols-go/secure-connection"

	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/globals"
)

func register(err error, packet nex.PacketInterface, callID uint32, vecMyURLs *types.List[*types.StationURL]) (*nex.RMCMessage, uint32) {
	if err != nil {
		common_globals.Logger.Error(err.Error())
		return nil, nex.ResultCodesCore.InvalidArgument
	}

	// TODO - This assumes a PRUDP connection. Refactor to support HPP
	connection := packet.Sender().(*nex.PRUDPConnection)
	endpoint := connection.Endpoint
	server := endpoint.Server

	// * vecMyURLs may contain multiple StationURLs. Search them all
	var localStation *types.StationURL
	var publicStation *types.StationURL

	for _, stationURL := range vecMyURLs.Slice() {
		natf := stationURL.Fields["natf"]
		natm := stationURL.Fields["natf"]
		pmp := stationURL.Fields["natf"]
		transportType := stationURL.Fields["type"]

		if natf == "0" && natm == "0" && pmp == "" && transportType == "" && localStation == nil {
			stationURL.SetLocal()
			localStation = stationURL.Copy().(*types.StationURL)
		}

		if transportType == "3" && publicStation == nil {
			stationURL.SetPublic()
			publicStation = stationURL.Copy().(*types.StationURL)
		}
	}

	if localStation == nil {
		common_globals.Logger.Error("Failed to find local station")
		return nil, nex.ResultCodesCore.InvalidArgument
	}

	if publicStation == nil {
		publicStation = localStation.Copy().(*types.StationURL)

		var address, port string

		switch clientAddress := connection.Address().(type) {
		case *net.UDPAddr:
		case *net.TCPAddr:
			address = clientAddress.IP.String()
			port = strconv.Itoa(clientAddress.Port)
		}

		publicStation.Fields["address"] = address
		publicStation.Fields["port"] = port
		publicStation.Fields["natf"] = "0"
		publicStation.Fields["natm"] = "0"
		publicStation.Fields["type"] = "3"
	}

	localStation.Fields["PID"] = strconv.Itoa(int(connection.PID().Value()))
	publicStation.Fields["PID"] = strconv.Itoa(int(connection.PID().Value()))

	localStation.Fields["RVCID"] = strconv.Itoa(int(connection.ID))
	publicStation.Fields["RVCID"] = strconv.Itoa(int(connection.ID))

	connection.StationURLs.Append(localStation)
	connection.StationURLs.Append(publicStation)

	retval := types.NewQResultSuccess(nex.ResultCodesCore.Unknown)
	pidConnectionID := types.NewPrimitiveU32(connection.ID)
	urlPublic := types.NewString(publicStation.EncodeToString())

	rmcResponseStream := nex.NewByteStreamOut(server)

	retval.WriteTo(rmcResponseStream)
	pidConnectionID.WriteTo(rmcResponseStream)
	urlPublic.WriteTo(rmcResponseStream)

	rmcResponseBody := rmcResponseStream.Bytes()

	rmcResponse := nex.NewRMCSuccess(server, rmcResponseBody)
	rmcResponse.ProtocolID = secure_connection.ProtocolID
	rmcResponse.MethodID = secure_connection.MethodRegister
	rmcResponse.CallID = callID

	return rmcResponse, 0
}
