package main

import (
	"fmt"
	"github.com/fatih/color"
	"net/rpc"
	"os"
)

func rpcClient(inMaintenanceMode bool){
	rpcPort := os.Getenv("RPC_PORT")
	c, err := rpc.Dial("tcp", "127.0.0.1:" + rpcPort)
	if err != nil {
		exitGracefully(err)
	}

	fmt.Println("Connected...")
	var result string
	err = c.Call("RPCServer.MaintenanceMode", inMaintenanceMode, &result)
	if err != nil {
		exitGracefully(err)
	}

	color.Yellow(result)
}
