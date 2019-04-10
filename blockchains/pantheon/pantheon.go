package pantheon

import (
	db "../../db"
	state "../../state"
	util "../../util"
	"context"
	"fmt"
	"github.com/Whiteblock/mustache"
	"golang.org/x/sync/semaphore"
	"log"
	"sync"
)

var conf *util.Config

func init() {
	conf = util.GetConfig()
}

func Build(details db.DeploymentDetails, servers []db.Server, clients []*util.SshClient,
	buildState *state.BuildState) ([]string, error) {

	sem := semaphore.NewWeighted(conf.ThreadLimit)
	ctx := context.TODO()
	mux := sync.Mutex{}

	panconf, err := NewConf(details.Params)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	buildState.SetBuildSteps(6*details.Nodes + 2)
	buildState.IncrementBuildProgress()

	addresses := make([]string, details.Nodes)
	pubKeys := make([]string, details.Nodes)
	privKeys := make([]string, details.Nodes)

	buildState.SetBuildStage("Setting Up Accounts")
	node := 0
	for i, server := range servers {
		for localId, _ := range server.Ips {
			sem.Acquire(ctx, 1)
			go func(i int, localId int, node int) {
				defer sem.Release(1)
				res, err := clients[i].DockerExec(localId, "pantheon --data-path=/pantheon/data public-key export-address --to=/pantheon/data/nodeAddress")
				if err != nil {
					log.Println(err)
					log.Println(res)
					buildState.ReportError(err)
					return
				}
				buildState.IncrementBuildProgress()
				_, err = clients[i].DockerExec(localId, "pantheon --data-path=/pantheon/data public-key export --to=/pantheon/data/publicKey")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}

				addr, err := clients[i].DockerExec(localId, "cat /pantheon/data/nodeAddress")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}

				addrs := string(addr[2:])

				mux.Lock()
				addresses[node] = addrs
				mux.Unlock()

				key, err := clients[i].DockerExec(localId, "cat /pantheon/data/publicKey")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				buildState.IncrementBuildProgress()

				mux.Lock()
				pubKeys[node] = key[2:]
				mux.Unlock()

				privKey, err := clients[i].DockerExec(localId, "cat /pantheon/data/key")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				mux.Lock()
				privKeys[node] = privKey[2:]
				mux.Unlock()

				res, err = clients[i].DockerExec(localId, "bash -c 'echo \"[\\\""+addrs+"\\\"]\" >> /pantheon/data/toEncode.json'")
				if err != nil {
					log.Println(err)
					log.Println(res)
					buildState.ReportError(err)
					return
				}

				_, err = clients[i].DockerExec(localId, "mkdir /pantheon/genesis")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}

				// used for IBFT2 extraData
				_, err = clients[i].DockerExec(localId, "pantheon rlp encode --from=/pantheon/data/toEncode.json --to=/pantheon/rlpEncodedExtraData")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				buildState.IncrementBuildProgress()
			}(i, localId, node)
			node++
		}
	}

	err = sem.Acquire(ctx, conf.ThreadLimit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sem.Release(conf.ThreadLimit)

	if !buildState.ErrorFree() {
		return nil, buildState.GetError()
	}
	sem.Acquire(ctx, 1)
	go func() {
		defer sem.Release(1)
		/*
		   Set up a geth node, which is not part of the blockchain network,
		   to sign the transactions in place of the pantheon client. The pantheon
		   client does not support wallet management, so this acts as an easy work around.
		*/
		err := startGeth(clients[0], panconf, addresses, privKeys, buildState)
		if err != nil {
			log.Println(err)
			buildState.ReportError(err)
			return
		}
	}()
	/* Create Genesis File */
	buildState.SetBuildStage("Generating Genesis File")
	err = createGenesisfile(panconf, details, addresses, buildState)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	//defer util.Rm("./genesis.json")

	p2pPort := 30303
	enodes := "["
	var enodeAddress string
	for _, server := range servers {
		for i, ip := range server.Ips {
			enodeAddress = fmt.Sprintf("enode://%s@%s:%d",
				pubKeys[i],
				ip,
				p2pPort)
			if i < len(pubKeys)-1 {
				enodes = enodes + "\"" + enodeAddress + "\"" + ","
			} else {
				enodes = enodes + "\"" + enodeAddress + "\""
			}
			buildState.IncrementBuildProgress()
		}
	}
	enodes = enodes + "]"

	/* Create Static Nodes File */
	buildState.SetBuildStage("Setting Up Static Peers")
	buildState.IncrementBuildProgress()
	err = createStaticNodesFile(enodes, buildState)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	//defer util.Rm("./static-nodes.json")

	/* Copy static-nodes & genesis files to each node */
	buildState.SetBuildStage("Distributing Files")
	for i, server := range servers {
		err = clients[i].Scp("static-nodes.json", "/home/appo/static-nodes.json")
		if err != nil {
			log.Println(err)
			return nil, err
		}
		defer clients[i].Run("rm /home/appo/static-nodes.json")

		err = clients[i].Scp("genesis.json", "/home/appo/genesis.json")
		if err != nil {
			log.Println(err)
			return nil, err
		}
		defer clients[i].Run("rm /home/appo/genesis.json")

		for localId, _ := range server.Ips {
			sem.Acquire(ctx, 1)
			go func(i int, localId int) {
				defer sem.Release(1)
				err := clients[i].DockerCp(localId, "/home/appo/static-nodes.json", "/pantheon/data/static-nodes.json")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				err = clients[i].DockerCp(localId, "/home/appo/genesis.json", "/pantheon/genesis/genesis.json")
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				buildState.IncrementBuildProgress()
			}(i, localId)
		}

	}

	err = sem.Acquire(ctx, conf.ThreadLimit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sem.Release(conf.ThreadLimit)

	if !buildState.ErrorFree() {
		return nil, buildState.GetError()
	}

	/* Start the nodes */
	buildState.SetBuildStage("Starting Pantheon")
	httpPort := 8545
	for i, server := range servers {
		for localId, _ := range server.Ips {
			sem.Acquire(ctx, 1)
			go func(i int, localId int) {
				defer sem.Release(1)
				err := clients[i].DockerExecdLog(localId, fmt.Sprintf(
					`pantheon --data-path /pantheon/data --genesis-file=/pantheon/genesis/genesis.json  `+
						`--rpc-http-enabled --rpc-http-api="ADMIN,CLIQUE,DEBUG,EEA,ETH,IBFT,MINER,NET,WEB3" `+
						` --p2p-port=%d --rpc-http-port=%d --rpc-http-host="0.0.0.0" --host-whitelist=all --rpc-http-cors-origins="*"`,
					p2pPort,
					httpPort))
				if err != nil {
					log.Println(err)
					buildState.ReportError(err)
					return
				}
				buildState.IncrementBuildProgress()
			}(i, localId)
		}
	}
	err = sem.Acquire(ctx, conf.ThreadLimit)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sem.Release(conf.ThreadLimit)

	if !buildState.ErrorFree() {
		return nil, buildState.GetError()
	}

	return privKeys, nil
}

func createGenesisfile(panconf *PanConf, details db.DeploymentDetails, address []string, buildState *state.BuildState) error {
	genesis := map[string]interface{}{
		"chainId":            panconf.NetworkId,
		"difficulty":         fmt.Sprintf("0x0%X", panconf.Difficulty),
		"gasLimit":           fmt.Sprintf("0x0%X", panconf.GasLimit),
		"blockPeriodSeconds": panconf.BlockPeriodSeconds,
		"epoch":              panconf.Epoch,
	}
	alloc := map[string]map[string]string{}
	for _, addr := range address {
		alloc[addr] = map[string]string{
			"balance": panconf.InitBalance,
		}
	}
	extraData := "0x0000000000000000000000000000000000000000000000000000000000000000"
	for _, addr := range address {
		extraData += addr
	}
	extraData += "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	genesis["extraData"] = extraData
	genesis["alloc"] = alloc
	dat, err := util.GetBlockchainConfig("pantheon", "genesis.json", details.Files)
	if err != nil {
		log.Println(err)
		return err
	}

	data, err := mustache.Render(string(dat), util.ConvertToStringMap(genesis))
	if err != nil {
		log.Println(err)
		return err
	}
	fmt.Println("Writing Genesis File Locally")
	return buildState.Write("genesis.json", data)

}

func createStaticNodesFile(list string, buildState *state.BuildState) error {
	return buildState.Write("static-nodes.json", list)
}

func startGeth(client *util.SshClient, panconf *PanConf, addresses []string, privKeys []string, buildState *state.BuildState) error {
	serviceIps, err := util.GetServiceIps(GetServices())
	if err != nil {
		log.Println(err)
		return err
	}

	err = buildState.SetExt("signer_ip", serviceIps["geth"])
	if err != nil {
		log.Println(err)
		return err
	}

	err = buildState.SetExt("accounts", addresses)
	if err != nil {
		log.Println(err)
		return err
	}

	//Set up a geth node as a service to sign transactions
	client.Run(`docker exec wb_service0 mkdir /geth/`)

	unlock := ""
	for i, privKey := range privKeys {

		res, err := client.Run(`docker exec wb_service0 bash -c 'echo "second" >> /geth/passwd'`)
		if err != nil {
			log.Println(res)
			log.Println(err)
			return err
		}
		res, err = client.Run(fmt.Sprintf(`docker exec wb_service0 bash -c 'echo -n "%s" > /geth/pk%d' `, privKey, i))
		if err != nil {
			log.Println(res)
			log.Println(err)
			return err
		}

		res, err = client.Run(fmt.Sprintf(`docker exec wb_service0 geth --datadir /geth/ account import --password /geth/passwd /geth/pk%d`, i))
		if err != nil {
			log.Println(res)
			log.Println(err)
			return err
		}

		if i != 0 {
			unlock += ","
		}
		unlock += "0x" + addresses[i]

	}
	res, err := client.Run(fmt.Sprintf(`docker exec -d wb_service0 geth --datadir /geth/ --rpc --rpcaddr 0.0.0.0`+
		` --rpcapi "admin,web3,db,eth,net,personal" --rpccorsdomain "0.0.0.0" --nodiscover --unlock="%s"`+
		` --password /geth/passwd --networkid %d`, unlock, panconf.NetworkId))
	if err != nil {
		log.Println(res)
		log.Println(err)
		return err
	}
	return nil
}