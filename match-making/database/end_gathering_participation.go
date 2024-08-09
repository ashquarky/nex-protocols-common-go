package database

import (
	"slices"

	"github.com/PretendoNetwork/nex-go/v2"
	"github.com/PretendoNetwork/nex-go/v2/types"
	common_globals "github.com/PretendoNetwork/nex-protocols-common-go/v2/globals"
	match_making "github.com/PretendoNetwork/nex-protocols-go/v2/match-making"
	notifications "github.com/PretendoNetwork/nex-protocols-go/v2/notifications"
	notifications_types "github.com/PretendoNetwork/nex-protocols-go/v2/notifications/types"
)

// EndGatheringParticipation ends the participation of a connection within a gathering and performs any additional handling required
func EndGatheringParticipation(manager *common_globals.MatchmakingManager, gatheringID uint32, connection *nex.PRUDPConnection, message string) *nex.Error {
	gathering, gatheringType, participants, _, nexError := FindGatheringByID(manager, gatheringID)
	if nexError != nil {
		return nexError
	}

	// TODO - Is this the right error?
	if !slices.Contains(participants, connection.PID().Value()) {
		return nex.NewError(nex.ResultCodes.RendezVous.NotParticipatedGathering, "change_error")
	}

	// * If the gathering is a PersistentGathering, only remove the participant from the gathering
	if gatheringType == "PersistentGathering" {
		_, nexError = RemoveParticipantFromGathering(manager, gatheringID, connection.PID().Value())
		return nexError
	}

	newParticipants, nexError := RemoveParticipantFromGathering(manager, gatheringID, connection.PID().Value())
	if nexError != nil {
		return nexError
	}

	if len(newParticipants) == 0 {
		// * There are no more participants, so we just unregister the gathering
		return UnregisterGathering(manager, gatheringID)
	}

	if connection.PID().Equals(gathering.OwnerPID) {
		// * This flag tells the server to change the matchmake session owner if they disconnect
		// * If the flag is not set, delete the session
		// * More info: https://nintendo-wiki.pretendo.network/docs/nex/protocols/match-making/types#flags
		if gathering.Flags.PAND(match_making.GatheringFlags.DisconnectChangeOwner) == 0 {
			nexError = UnregisterGathering(manager, gatheringID)
			if nexError != nil {
				return nexError
			}

			category := notifications.NotificationCategories.GatheringUnregistered
			subtype := notifications.NotificationSubTypes.GatheringUnregistered.None

			oEvent := notifications_types.NewNotificationEvent()
			oEvent.PIDSource = connection.PID().Copy().(*types.PID)
			oEvent.Type.Value = notifications.BuildNotificationType(category, subtype)
			oEvent.Param1.Value = gatheringID

			common_globals.SendNotificationEvent(connection.Endpoint().(*nex.PRUDPEndPoint), oEvent, common_globals.RemoveDuplicates(newParticipants))

			return nil
		}

		nexError = MigrateGatheringOwnership(manager, connection, gathering, newParticipants)
		if nexError != nil {
			return nexError
		}
	}

	category := notifications.NotificationCategories.Participation
	subtype := notifications.NotificationSubTypes.Participation.Ended

	oEvent := notifications_types.NewNotificationEvent()
	oEvent.PIDSource = connection.PID().Copy().(*types.PID)
	oEvent.Type = types.NewPrimitiveU32(notifications.BuildNotificationType(category, subtype))
	oEvent.Param1.Value = gatheringID
	oEvent.Param2.Value = connection.PID().LegacyValue() // TODO - This assumes a legacy client. Will not work on the Switch
	oEvent.StrParam.Value = message

	var participationEndedTargets []uint64

	// * When the VerboseParticipants or VerboseParticipantsEx flags are set, all participant notification events are sent to everyone
	if gathering.Flags.PAND(match_making.GatheringFlags.VerboseParticipants | match_making.GatheringFlags.VerboseParticipantsEx) != 0 {
		participationEndedTargets = common_globals.RemoveDuplicates(participants)
	} else {
		participationEndedTargets = []uint64{gathering.OwnerPID.Value()}
	}

	common_globals.SendNotificationEvent(connection.Endpoint().(*nex.PRUDPEndPoint), oEvent, participationEndedTargets)

	return nil
}