package handlers

/*
	Sliver Implant Framework
	Copyright (C) 2021  Bishop Fox

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <https://www.gnu.org/licenses/>.
	------------------------------------------------------------------------

	WARNING: These functions can be invoked by remote implants without user interaction

*/

import (
	"sync"

	"github.com/bishopfox/sliver/protobuf/sliverpb"
	"github.com/bishopfox/sliver/server/core"
)

type ServerHandler func(*core.ImplantConnection, []byte)

var (
	serverHandlers = map[uint32]ServerHandler{
		// Sessions
		sliverpb.MsgRegister:    registerSessionHandler,
		sliverpb.MsgTunnelData:  tunnelDataHandler,
		sliverpb.MsgTunnelClose: tunnelCloseHandler,
		sliverpb.MsgPing:        pingHandler,

		// Beacons
		sliverpb.MsgRegisterBeacon: beaconRegisterHandler,
		sliverpb.MsgBeaconTasks:    beaconTasksHandler,
	}

	tunnelHandlerMutex = &sync.Mutex{}
)

// GetHandlers - Returns a map of server-side msg handlers
func GetHandlers() map[uint32]ServerHandler {
	return serverHandlers
}

// AddHandler -  Adds a new handler to the map of server-side msg handlers
func AddHandler(key uint32, value ServerHandler) {
	serverHandlers[key] = value
}
