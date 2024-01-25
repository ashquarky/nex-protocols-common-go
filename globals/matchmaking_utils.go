package common_globals

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"

	"github.com/PretendoNetwork/nex-go"
	"github.com/PretendoNetwork/nex-go/types"
	match_making "github.com/PretendoNetwork/nex-protocols-go/match-making"
	match_making_types "github.com/PretendoNetwork/nex-protocols-go/match-making/types"
	notifications "github.com/PretendoNetwork/nex-protocols-go/notifications"
	notifications_types "github.com/PretendoNetwork/nex-protocols-go/notifications/types"
	"golang.org/x/exp/slices"
)

// GetAvailableGatheringID returns a gathering ID which doesn't belong to any session
// Returns 0 if no IDs are available (math.MaxUint32 has been reached)
func GetAvailableGatheringID() uint32 {
	return CurrentGatheringID.Next()
}

// FindOtherConnectionID searches a connection ID on the session that isn't the given one
// Returns 0 if no connection ID could be found
func FindOtherConnectionID(excludedConnectionID uint32, gatheringID uint32) uint32 {
	for _, connectionID := range Sessions[gatheringID].ConnectionIDs {
		if connectionID != excludedConnectionID {
			return connectionID
		}
	}

	return 0
}

// RemoveConnectionIDFromSession removes a PRUDP connection from the session
func RemoveConnectionIDFromSession(id uint32, gathering uint32) {
	for index, connectionID := range Sessions[gathering].ConnectionIDs {
		if connectionID == id {
			Sessions[gathering].ConnectionIDs = DeleteIndex(Sessions[gathering].ConnectionIDs, index)
		}
	}

	if len(Sessions[gathering].ConnectionIDs) == 0 {
		delete(Sessions, gathering)
	}
}

// FindConnectionSession searches for session the given connection ID is connected to
func FindConnectionSession(id uint32) uint32 {
	for gatheringID := range Sessions {
		if slices.Contains(Sessions[gatheringID].ConnectionIDs, id) {
			return gatheringID
		}
	}

	return 0
}

// RemoveConnectionFromAllSessions removes a connection from every session
func RemoveConnectionFromAllSessions(connection *nex.PRUDPConnection) {
	// * Keep checking until no session is found
	for gid := FindConnectionSession(connection.ID); gid != 0; {
		session := Sessions[gid]
		lenParticipants := len(session.ConnectionIDs)

		RemoveConnectionIDFromSession(connection.ID, gid)

		if lenParticipants <= 1 {
			gid = FindConnectionSession(connection.ID)
			continue
		}

		ownerPID := session.GameMatchmakeSession.Gathering.OwnerPID

		if ownerPID.Equals(connection.PID()) {
			// * This flag tells the server to change the matchmake session owner if they disconnect
			// * If the flag is not set, delete the session
			// * More info: https://nintendo-wiki.pretendo.network/docs/nex/protocols/match-making/types#flags
			if session.GameMatchmakeSession.Gathering.Flags.PAND(match_making.GatheringFlags.DisconnectChangeOwner) == 0 {
				delete(Sessions, gid)
			} else {
				ChangeSessionOwner(connection, gid)
			}
		} else {
			endpoint := connection.Endpoint
			server := endpoint.Server

			category := notifications.NotificationCategories.Participation
			subtype := notifications.NotificationSubTypes.Participation.Disconnected

			oEvent := notifications_types.NewNotificationEvent()
			oEvent.PIDSource = connection.PID()
			oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(category, subtype))
			oEvent.Param1 = types.NewPrimitiveU32(gid)
			oEvent.Param2 = types.NewPrimitiveU32(connection.PID().LegacyValue()) // TODO - This assumes a legacy client. This won't work on the Switch

			stream := nex.NewByteStreamOut(server)

			oEvent.WriteTo(stream)

			rmcRequest := nex.NewRMCRequest(server)
			rmcRequest.ProtocolID = notifications.ProtocolID
			rmcRequest.CallID = CurrentMatchmakingCallID.Next()
			rmcRequest.MethodID = notifications.MethodProcessNotificationEvent
			rmcRequest.Parameters = stream.Bytes()

			rmcRequestBytes := rmcRequest.Bytes()

			target := endpoint.FindConnectionByPID(ownerPID.Value())
			if target == nil {
				Logger.Warning("Target connection not found")
				gid = FindConnectionSession(connection.ID)
				continue
			}

			var messagePacket nex.PRUDPPacketInterface

			if connection.DefaultPRUDPVersion == 0 {
				messagePacket, _ = nex.NewPRUDPPacketV0(target, nil)
			} else {
				messagePacket, _ = nex.NewPRUDPPacketV1(target, nil)
			}

			messagePacket.SetType(nex.DataPacket)
			messagePacket.AddFlag(nex.FlagNeedsAck)
			messagePacket.AddFlag(nex.FlagReliable)
			messagePacket.SetSourceVirtualPortStreamType(connection.StreamType)
			messagePacket.SetSourceVirtualPortStreamID(connection.Endpoint.StreamID)
			messagePacket.SetDestinationVirtualPortStreamType(connection.StreamType)
			messagePacket.SetDestinationVirtualPortStreamID(connection.StreamID)
			messagePacket.SetPayload(rmcRequestBytes)

			server.Send(messagePacket)
		}

		gid = FindConnectionSession(connection.ID)
	}
}

// CreateSessionByMatchmakeSession creates a gathering from a MatchmakeSession
func CreateSessionByMatchmakeSession(matchmakeSession *match_making_types.MatchmakeSession, searchMatchmakeSession *match_making_types.MatchmakeSession, hostPID *types.PID) (*CommonMatchmakeSession, error, uint32) {
	sessionIndex := GetAvailableGatheringID()
	if sessionIndex == 0 {
		sessionIndex = GetAvailableGatheringID() // * Skip to index 1
	}

	session := CommonMatchmakeSession{
		SearchMatchmakeSession: searchMatchmakeSession,
		GameMatchmakeSession:   matchmakeSession,
	}

	session.GameMatchmakeSession.Gathering.ID = types.NewPrimitiveU32(sessionIndex)
	session.GameMatchmakeSession.Gathering.OwnerPID = hostPID
	session.GameMatchmakeSession.Gathering.HostPID = hostPID

	session.GameMatchmakeSession.StartedTime = types.NewDateTime(0).Now()
	session.GameMatchmakeSession.SessionKey = types.NewBuffer(make([]byte, 32))

	rand.Read(session.GameMatchmakeSession.SessionKey.Value)

	if session.GameMatchmakeSession.MatchmakeParam == nil {
		session.GameMatchmakeSession.MatchmakeParam = match_making_types.NewMatchmakeParam()
	}

	if session.GameMatchmakeSession.MatchmakeParam.Params == nil {
		session.GameMatchmakeSession.MatchmakeParam.Params = types.NewMap[*types.String, *types.Variant]()
		session.GameMatchmakeSession.MatchmakeParam.Params.KeyType = types.NewString("")
		session.GameMatchmakeSession.MatchmakeParam.Params.ValueType = types.NewVariant()
	}

	SR := types.NewVariant()
	SR.TypeID = types.NewPrimitiveU8(3)
	SR.Type = types.NewPrimitiveBool(true)

	GIR := types.NewVariant()
	GIR.TypeID = types.NewPrimitiveU8(1)
	GIR.Type = types.NewPrimitiveS64(3)

	session.GameMatchmakeSession.MatchmakeParam.Params.Set(types.NewString("@SR"), SR)
	session.GameMatchmakeSession.MatchmakeParam.Params.Set(types.NewString("@GIR"), GIR)

	Sessions[sessionIndex] = &session

	return Sessions[sessionIndex], nil, 0
}

// FindSessionByMatchmakeSession finds a gathering that matches with a MatchmakeSession
func FindSessionByMatchmakeSession(pid *types.PID, searchMatchmakeSession *match_making_types.MatchmakeSession) uint32 {
	// * This portion finds any sessions that match the search session
	// * It does not care about anything beyond that, such as if the match is already full
	// * This is handled below
	candidateSessionIndexes := make([]uint32, 0, len(Sessions))
	for index, session := range Sessions {
		if session.SearchMatchmakeSession.Equals(searchMatchmakeSession) {
			candidateSessionIndexes = append(candidateSessionIndexes, index)
		}
	}

	// TODO - This whole section assumes legacy clients. None of it will work on the Switch
	var friendList []uint32
	for _, sessionIndex := range candidateSessionIndexes {
		sessionToCheck := Sessions[sessionIndex]
		if len(sessionToCheck.ConnectionIDs) >= int(sessionToCheck.GameMatchmakeSession.MaximumParticipants.Value) {
			continue
		}

		if !sessionToCheck.GameMatchmakeSession.OpenParticipation.Value {
			continue
		}

		// * If the session only allows friends, check if the owner is in the friend list of the PID
		// TODO - Is this a flag or a constant?
		if sessionToCheck.GameMatchmakeSession.ParticipationPolicy.Value == 98 {
			if GetUserFriendPIDsHandler == nil {
				Logger.Warning("Missing GetUserFriendPIDsHandler!")
				continue
			}

			if len(friendList) == 0 {
				friendList = GetUserFriendPIDsHandler(pid.LegacyValue()) // TODO - This grpc method needs to support the Switch
			}

			if !slices.Contains(friendList, sessionToCheck.GameMatchmakeSession.OwnerPID.LegacyValue()) {
				continue
			}
		}

		return sessionIndex // * Found a match
	}

	return 0
}

// FindSessionsByMatchmakeSessionSearchCriterias finds a gathering that matches with the given search criteria
func FindSessionsByMatchmakeSessionSearchCriterias(pid *types.PID, searchCriterias []*match_making_types.MatchmakeSessionSearchCriteria, gameSpecificChecks func(searchCriteria *match_making_types.MatchmakeSessionSearchCriteria, matchmakeSession *match_making_types.MatchmakeSession) bool) []*CommonMatchmakeSession {
	candidateSessions := make([]*CommonMatchmakeSession, 0, len(Sessions))

	// TODO - This whole section assumes legacy clients. None of it will work on the Switch
	var friendList []uint32
	for _, session := range Sessions {
		for _, criteria := range searchCriterias {
			// * Check things like game specific attributes
			if gameSpecificChecks != nil {
				if !gameSpecificChecks(criteria, session.GameMatchmakeSession) {
					continue
				}
			} else {
				if !compareAttributesSearchCriteria(session.GameMatchmakeSession.Attributes.Slice(), criteria.Attribs.Slice()) {
					continue
				}
			}

			if !compareSearchCriteria(session.GameMatchmakeSession.MaximumParticipants.Value, criteria.MaxParticipants.Value) {
				continue
			}

			if !compareSearchCriteria(session.GameMatchmakeSession.MinimumParticipants.Value, criteria.MinParticipants.Value) {
				continue
			}

			if !compareSearchCriteria(session.GameMatchmakeSession.GameMode.Value, criteria.GameMode.Value) {
				continue
			}

			if len(session.ConnectionIDs) >= int(session.GameMatchmakeSession.MaximumParticipants.Value) {
				continue
			}

			if !session.GameMatchmakeSession.OpenParticipation.Value {
				continue
			}

			// * If the session only allows friends, check if the owner is in the friend list of the PID
			// TODO - Is this a flag or a constant?
			if session.GameMatchmakeSession.ParticipationPolicy.Value == 98 {
				if GetUserFriendPIDsHandler == nil {
					Logger.Warning("Missing GetUserFriendPIDsHandler!")
					continue
				}

				if len(friendList) == 0 {
					friendList = GetUserFriendPIDsHandler(pid.LegacyValue()) // TODO - Support the Switch
				}

				if !slices.Contains(friendList, session.GameMatchmakeSession.OwnerPID.LegacyValue()) {
					continue
				}
			}

			candidateSessions = append(candidateSessions, session)

			// We don't have to compare with other search criterias
			break
		}
	}

	return candidateSessions
}

func compareAttributesSearchCriteria(original []*types.PrimitiveU32, search []*types.String) bool {
	if len(original) != len(search) {
		return false
	}

	for index, originalAttribute := range original {
		searchAttribute := search[index]

		if !compareSearchCriteria(originalAttribute.Value, searchAttribute.Value) {
			return false
		}
	}

	return true
}

func compareSearchCriteria[T ~uint16 | ~uint32](original T, search string) bool {
	if search == "" { // * Accept any value
		return true
	}

	before, after, found := strings.Cut(search, ",")
	if found {
		min, err := strconv.ParseUint(before, 10, 64)
		if err != nil {
			return false
		}

		max, err := strconv.ParseUint(after, 10, 64)
		if err != nil {
			return false
		}

		return min <= uint64(original) && max >= uint64(original)
	} else {
		searchNum, err := strconv.ParseUint(before, 10, 64)
		if err != nil {
			return false
		}

		return searchNum == uint64(original)
	}
}

// AddPlayersToSession updates the given sessions state to include the provided connection IDs
// Returns a NEX error code if failed
func AddPlayersToSession(session *CommonMatchmakeSession, connectionIDs []uint32, initiatingConnection *nex.PRUDPConnection, joinMessage string) (error, uint32) {
	if (len(session.ConnectionIDs) + len(connectionIDs)) > int(session.GameMatchmakeSession.Gathering.MaximumParticipants.Value) {
		return fmt.Errorf("Gathering %d is full", session.GameMatchmakeSession.Gathering.ID), nex.ResultCodes.RendezVous.SessionFull
	}

	for _, connectedID := range connectionIDs {
		if slices.Contains(session.ConnectionIDs, connectedID) {
			return fmt.Errorf("Connection ID %d is already in gathering %d", connectedID, session.GameMatchmakeSession.Gathering.ID), nex.ResultCodes.RendezVous.AlreadyParticipatedGathering
		}

		session.ConnectionIDs = append(session.ConnectionIDs, connectedID)

		session.GameMatchmakeSession.ParticipationCount.Value += 1
	}

	endpoint := initiatingConnection.Endpoint
	server := endpoint.Server

	for i := 0; i < len(session.ConnectionIDs); i++ {
		target := endpoint.FindConnectionByID(session.ConnectionIDs[i])
		if target == nil {
			// TODO - Error here?
			Logger.Warning("Player not found")
			continue
		}

		notificationCategory := notifications.NotificationCategories.Participation
		notificationSubtype := notifications.NotificationSubTypes.Participation.NewParticipant

		oEvent := notifications_types.NewNotificationEvent()
		oEvent.PIDSource = initiatingConnection.PID()
		oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(notificationCategory, notificationSubtype))
		oEvent.Param1 = session.GameMatchmakeSession.ID.Copy().(*types.PrimitiveU32)
		oEvent.Param2 = types.NewPrimitiveU32(target.PID().LegacyValue()) // TODO - This assumes a legacy client. Will not work on the Switch
		oEvent.StrParam = types.NewString(joinMessage)
		oEvent.Param3 = types.NewPrimitiveU32(uint32(len(connectionIDs)))

		notificationStream := nex.NewByteStreamOut(server)

		oEvent.WriteTo(notificationStream)

		notificationRequest := nex.NewRMCRequest(server)
		notificationRequest.ProtocolID = notifications.ProtocolID
		notificationRequest.CallID = CurrentMatchmakingCallID.Next()
		notificationRequest.MethodID = notifications.MethodProcessNotificationEvent
		notificationRequest.Parameters = notificationStream.Bytes()

		notificationRequestBytes := notificationRequest.Bytes()

		var messagePacket nex.PRUDPPacketInterface

		if target.DefaultPRUDPVersion == 0 {
			messagePacket, _ = nex.NewPRUDPPacketV0(target, nil)
		} else {
			messagePacket, _ = nex.NewPRUDPPacketV1(target, nil)
		}

		messagePacket.SetType(nex.DataPacket)
		messagePacket.AddFlag(nex.FlagNeedsAck)
		messagePacket.AddFlag(nex.FlagReliable)
		messagePacket.SetSourceVirtualPortStreamType(target.StreamType)
		messagePacket.SetSourceVirtualPortStreamID(target.Endpoint.StreamID)
		messagePacket.SetDestinationVirtualPortStreamType(target.StreamType)
		messagePacket.SetDestinationVirtualPortStreamID(target.StreamID)
		messagePacket.SetPayload(notificationRequestBytes)

		server.Send(messagePacket)
	}

	// * This appears to be correct. Tri-Force Heroes uses 3.9.0,
	// * and has issues if these notifications are sent.
	// * Minecraft, however, requires these to be sent
	// TODO - Check other games both pre and post 3.10.0 and validate
	if server.MatchMakingProtocolVersion().GreaterOrEqual("3.10.0") {
		for i := 0; i < len(session.ConnectionIDs); i++ {
			target := endpoint.FindConnectionByID(session.ConnectionIDs[i])
			if target == nil {
				// TODO - Error here?
				Logger.Warning("Player not found")
				continue
			}

			notificationCategory := notifications.NotificationCategories.Participation
			notificationSubtype := notifications.NotificationSubTypes.Participation.NewParticipant

			oEvent := notifications_types.NewNotificationEvent()
			oEvent.PIDSource = initiatingConnection.PID()
			oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(notificationCategory, notificationSubtype))
			oEvent.Param1 = session.GameMatchmakeSession.ID.Copy().(*types.PrimitiveU32)
			oEvent.Param2 = types.NewPrimitiveU32(target.PID().LegacyValue()) // TODO - This assumes a legacy client. Will not work on the Switch
			oEvent.StrParam = types.NewString(joinMessage)
			oEvent.Param3 = types.NewPrimitiveU32(uint32(len(connectionIDs)))

			notificationStream := nex.NewByteStreamOut(server)

			oEvent.WriteTo(notificationStream)

			notificationRequest := nex.NewRMCRequest(server)
			notificationRequest.ProtocolID = notifications.ProtocolID
			notificationRequest.CallID = CurrentMatchmakingCallID.Next()
			notificationRequest.MethodID = notifications.MethodProcessNotificationEvent
			notificationRequest.Parameters = notificationStream.Bytes()

			notificationRequestBytes := notificationRequest.Bytes()

			var messagePacket nex.PRUDPPacketInterface

			if target.DefaultPRUDPVersion == 0 {
				messagePacket, _ = nex.NewPRUDPPacketV0(initiatingConnection, nil)
			} else {
				messagePacket, _ = nex.NewPRUDPPacketV1(initiatingConnection, nil)
			}

			messagePacket.SetType(nex.DataPacket)
			messagePacket.AddFlag(nex.FlagNeedsAck)
			messagePacket.AddFlag(nex.FlagReliable)
			messagePacket.SetSourceVirtualPortStreamType(target.StreamType)
			messagePacket.SetSourceVirtualPortStreamID(target.Endpoint.StreamID)
			messagePacket.SetDestinationVirtualPortStreamType(target.StreamType)
			messagePacket.SetDestinationVirtualPortStreamID(target.StreamID)
			messagePacket.SetPayload(notificationRequestBytes)

			server.Send(messagePacket)
		}

		notificationCategory := notifications.NotificationCategories.Participation
		notificationSubtype := notifications.NotificationSubTypes.Participation.NewParticipant

		oEvent := notifications_types.NewNotificationEvent()
		oEvent.PIDSource = initiatingConnection.PID()
		oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(notificationCategory, notificationSubtype))
		oEvent.Param1 = session.GameMatchmakeSession.ID
		oEvent.Param2 = types.NewPrimitiveU32(initiatingConnection.PID().LegacyValue()) // TODO - This assumes a legacy client. Will not work on the Switch
		oEvent.StrParam = types.NewString(joinMessage)
		oEvent.Param3 = types.NewPrimitiveU32(uint32(len(connectionIDs)))

		notificationStream := nex.NewByteStreamOut(server)

		oEvent.WriteTo(notificationStream)

		notificationRequest := nex.NewRMCRequest(server)
		notificationRequest.ProtocolID = notifications.ProtocolID
		notificationRequest.CallID = CurrentMatchmakingCallID.Next()
		notificationRequest.MethodID = notifications.MethodProcessNotificationEvent
		notificationRequest.Parameters = notificationStream.Bytes()

		notificationRequestBytes := notificationRequest.Bytes()

		var messagePacket nex.PRUDPPacketInterface

		if initiatingConnection.DefaultPRUDPVersion == 0 {
			messagePacket, _ = nex.NewPRUDPPacketV0(initiatingConnection, nil)
		} else {
			messagePacket, _ = nex.NewPRUDPPacketV1(initiatingConnection, nil)
		}

		messagePacket.SetType(nex.DataPacket)
		messagePacket.AddFlag(nex.FlagNeedsAck)
		messagePacket.AddFlag(nex.FlagReliable)
		messagePacket.SetSourceVirtualPortStreamType(initiatingConnection.StreamType)
		messagePacket.SetSourceVirtualPortStreamID(initiatingConnection.Endpoint.StreamID)
		messagePacket.SetDestinationVirtualPortStreamType(initiatingConnection.StreamType)
		messagePacket.SetDestinationVirtualPortStreamID(initiatingConnection.StreamID)
		messagePacket.SetPayload(notificationRequestBytes)

		server.Send(messagePacket)

		target := endpoint.FindConnectionByPID(session.GameMatchmakeSession.Gathering.OwnerPID.Value())
		if target == nil {
			// TODO - Error here?
			Logger.Warning("Player not found")
			return nil, 0
		}

		if target.DefaultPRUDPVersion == 0 {
			messagePacket, _ = nex.NewPRUDPPacketV0(target, nil)
		} else {
			messagePacket, _ = nex.NewPRUDPPacketV1(target, nil)
		}

		messagePacket.SetType(nex.DataPacket)
		messagePacket.AddFlag(nex.FlagNeedsAck)
		messagePacket.AddFlag(nex.FlagReliable)
		messagePacket.SetSourceVirtualPortStreamType(target.StreamType)
		messagePacket.SetSourceVirtualPortStreamID(target.Endpoint.StreamID)
		messagePacket.SetDestinationVirtualPortStreamType(target.StreamType)
		messagePacket.SetDestinationVirtualPortStreamID(target.StreamID)
		messagePacket.SetPayload(notificationRequestBytes)

		server.Send(messagePacket)
	}

	return nil, 0
}

// ChangeSessionOwner changes the session owner to a different connection
func ChangeSessionOwner(currentOwner *nex.PRUDPConnection, gathering uint32) {
	endpoint := currentOwner.Endpoint
	server := endpoint.Server

	var newOwner *nex.PRUDPConnection

	newOwnerConnectionID := FindOtherConnectionID(currentOwner.ID, gathering)
	if newOwnerConnectionID != 0 {
		newOwner = endpoint.FindConnectionByID(newOwnerConnectionID)
		if newOwner == nil {
			Logger.Warning("Other connection not found")
			return
		}

		Sessions[gathering].GameMatchmakeSession.Gathering.OwnerPID = newOwner.PID()
	} else {
		return
	}

	category := notifications.NotificationCategories.OwnershipChanged
	subtype := notifications.NotificationSubTypes.OwnershipChanged.None

	oEvent := notifications_types.NewNotificationEvent()
	oEvent.PIDSource = newOwner.PID()
	oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(category, subtype))
	oEvent.Param1 = types.NewPrimitiveU32(gathering)
	oEvent.Param2 = types.NewPrimitiveU32(newOwner.PID().LegacyValue()) // TODO - This assumes a legacy client. Will not work on the Switch

	// TODO - StrParam doesn't have this value on some servers
	// * https://github.com/kinnay/NintendoClients/issues/101
	// * unixTime := time.Now()
	// * oEvent.StrParam = strconv.FormatInt(unixTime.UnixMicro(), 10)

	stream := nex.NewByteStreamOut(server)

	oEvent.WriteTo(stream)

	rmcRequest := nex.NewRMCRequest(server)
	rmcRequest.ProtocolID = notifications.ProtocolID
	rmcRequest.CallID = CurrentMatchmakingCallID.Next()
	rmcRequest.MethodID = notifications.MethodProcessNotificationEvent
	rmcRequest.Parameters = stream.Bytes()

	rmcRequestBytes := rmcRequest.Bytes()

	for _, connectionID := range Sessions[gathering].ConnectionIDs {
		target := endpoint.FindConnectionByID(connectionID)
		if target == nil {
			Logger.Warning("Connection not found")
			continue
		}

		var messagePacket nex.PRUDPPacketInterface

		if target.DefaultPRUDPVersion == 0 {
			messagePacket, _ = nex.NewPRUDPPacketV0(target, nil)
		} else {
			messagePacket, _ = nex.NewPRUDPPacketV1(target, nil)
		}

		messagePacket.SetType(nex.DataPacket)
		messagePacket.AddFlag(nex.FlagNeedsAck)
		messagePacket.AddFlag(nex.FlagReliable)
		messagePacket.SetSourceVirtualPortStreamType(target.StreamType)
		messagePacket.SetSourceVirtualPortStreamID(target.Endpoint.StreamID)
		messagePacket.SetDestinationVirtualPortStreamType(target.StreamType)
		messagePacket.SetDestinationVirtualPortStreamID(target.StreamID)
		messagePacket.SetPayload(rmcRequestBytes)

		server.Send(messagePacket)
	}
}
