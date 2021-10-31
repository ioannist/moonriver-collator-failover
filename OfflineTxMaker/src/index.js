const AWS = require('aws-sdk')
const { ApiPromise, Keyring, WsProvider } = require("@polkadot/api");
const { typesBundle } = require("./moonbeam-types-bundle");
const crypto = require("crypto");
const createSignedTx = require("@substrate/txwrapper-core/lib/core/construct").createSignedTx;
const createSigningPayload = require("@substrate/txwrapper-core/lib/core/construct").createSigningPayload;
const createMetadata = require("@substrate/txwrapper-core").createMetadata;
const getRegistry = require("@substrate/txwrapper-registry").getRegistry;
const defineMethod = require('@substrate/txwrapper-core').defineMethod;
const EXTRINSIC_VERSION = require("@polkadot/types/extrinsic/v4/Extrinsic").EXTRINSIC_VERSION;

// secretkey_movrfailover1
const keyCaller = 'YOUR_GREEN_KEY';
const keyEnv = 'YOUR_GRAY_KEY';
const gatekeeperAccessKeyID = 'YOUR_AWS_GATEKEEPER_IAM_ACCESS_KEY'; // orange key
const gatekeeperSecret = 'YOUR_AWS_GATEKEEPER_IAM_SECRET'; // orange key
const telemetryWatcherDynamoAccessKeyID = 'YOUR_AWS_TELEMETRYWATCHER_DYNAMO_IAM_ACCESS_KEY';
const telemetryWatcherDynamoSecret = 'YOUR_AWS_TELEMETRY_WATCHER_DYANMO_IAM_SECRET';
const account = 'YOUR_COLLATOR-PUBLIC-ADDRESS';
const masterKey = 'YOUR-PROXY_PRIVATE_KEY' // red key

// const TX_VALIDITY_IN_DAYS = 30 * 1; // for how many days will this transaction be avlid
const NONCES_AHEAD = 30; // for how many nonces ahead of the current nonce should we generate valid transactions for
const KMS_ARN = "arn:aws:kms:eu-central-1:XXXXXXXXXXXXXX:key/YOUR-KEY-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
const MOVR = 1_000_000_000_000_000_000n; // MOVR denomination

var kms = new AWS.KMS(new AWS.Config({
  region: "eu-central-1",
  accessKeyId: gatekeeperAccessKeyID, secretAccessKey: gatekeeperSecret,
}));
let documentClient = new AWS.DynamoDB.DocumentClient(new AWS.Config({
  region: "eu-central-1",
  accessKeyId: telemetryWatcherDynamoAccessKeyID, secretAccessKey: telemetryWatcherDynamoSecret,
}));


/**
 * This program will download all node session information from DB:
 * nodeName: string; a unique human-readable identifier of our node that also shows up on Telemetry
 * groupName: string; the network or chain, i.e. moonriver; required to be able to monitor multiple networks
 * transactions: string; a json string that stores all potential encrypted presigned raw transactions
 * session: string; the session ID of the node (the one we use in authorMapping.updateAssociation)
 * 
 * The program will then generate NONCES_AHEAD raw transaction strings (while incrementing the nonce)
 * for every possible pair session1 -> session2
 * It will sign these transactions using your private key to generate signed raw transaction strings
 * It will then encrypt these signed transactions using our own encryption for safe storage
 * Finally, it will save the encrypted signed transactions in the database for future use
 * Note that the transactions are never transmitted to the network, i.e. they are not executed.
 */
(async function () {
  try {
    console.log('Getting sessions from db')
    const sessions = await scanSessions()
    let nodeNamesToSessionPairs = new Map()

    for (const from of sessions) {
      nodeNamesToSessionPairs.set(from.nodeName, {})
      for (const to of sessions) {
        if (from.nodeName === to.nodeName) {
          continue // no point in swithing to the same node/session
        }
        nodeNamesToSessionPairs.set(from.nodeName, {
          ...nodeNamesToSessionPairs.get(from.nodeName),
          [to.nodeName]: [from.session, to.session]
        });
      }
    }

    console.log('Generating txs')
    // console.log(nodeNamesToSessionPairs)
    const fromToTx = await makeOfflineTx(nodeNamesToSessionPairs);

    console.log('Updating signed txs in db')
    console.log(fromToTx)
    for (const [fromNodeName, txs] of fromToTx) {
      await updateSessionSignedTxs(fromNodeName, JSON.stringify(txs))
    }
    console.log('Finished')

  } catch (e) {
    console.error(e)
  }
})();


async function makeOfflineTx(nodeNamesToSessionPairs) {
  const api = await providePolkadotApi();
  await api.isReady;
  await new Promise((resolve) => {
    setTimeout(resolve, 100);
  });

  const { block } = await api.rpc.chain.getBlock();
  const blockHash = (await api.rpc.chain.getBlockHash(block.header.number)).toHex();
  const genesisHash = (await api.rpc.chain.getBlockHash(0)).toHex();
  const metadataRpc = (await api.rpc.state.getMetadata()).toHex();
  const nonce = +(await api.derive.balances.account(account)).accountNonce

  const { specVersion, transactionVersion, specName } = await api.rpc.state.getRuntimeVersion();
  const registry = getRegistry({
    chainName: "Moonriver",
    specName,
    specVersion,
    metadataRpc,
  });

  const blockNumber = registry.createType("BlockNumber", block.header.number).toNumber();
  // const eraPeriod = Math.floor(TX_VALIDITY_IN_DAYS * 24 * 60 * 60 / 12);
  
  const fromToTx = new Map()
  // make transactions for all nodes/sessions (this is the 'old' in authorMapping.updateAssociation)
  for (const [from, value] of nodeNamesToSessionPairs) {
    fromToTx.set(from, {})
    // make transactions for all possible new sessions (this is the 'new' in authorMapping.updateAssociation)
    for (const to in value) {
      const [sessionFrom, sessionTo] = value[to]
      console.log([sessionFrom, sessionTo])
      // make a transaction for all future nonces, up to current + NONCES_AHEAD
      // this is necessary to ensure we can execute the transaction even if the nonce has changed
      const nonceTxs = [];
      for (let n = nonce; n < nonce + NONCES_AHEAD; n++) {
        console.log(`${from} -> ${to}, ${n}`)
        const unsigned = getUpdateAssociationTx(
          {
            oldAuthorId: sessionFrom,
            newAuthorId: sessionTo
          },
          {
            // Read parameter descriptions at
            // https://github.com/paritytech/txwrapper-core/blob/b213cabf50f18f0fe710817072a81596e1a53cae/packages/txwrapper-core/src/types/method.ts
            address: account,
            blockHash, // The checkpoint hash of the block, in hex.
            blockNumber, // The checkpoint block number (u32), in hex.
            eraPeriod: undefined, // number of blocks from checkpoint that transaction is valid
            genesisHash,
            metadataRpc,
            nonce: n,
            specVersion,
            tip: 0,
            transactionVersion,
          },
          {
            metadataRpc,
            registry,
          }
        );

        const signingPayload = createSigningPayload(unsigned, { registry });
        const keyring = new Keyring({ type: "ethereum" });
        const dvnKey = keyring.addFromUri(masterKey, null, "ethereum");
        const signature = signWith(dvnKey, signingPayload, {
          metadataRpc,
          registry,
        });
        // Serialize a signed transaction.
        const tx = createSignedTx(unsigned, signature, { metadataRpc, registry });
        
        console.log(tx)
        
        let encrypted = encrypt(keyCaller, `movrfailover_${tx}`)
        encrypted = encrypt(keyEnv, encrypted)
        encrypted = await kmsEncrypt(encrypted)
        nonceTxs.push(encrypted)
      }

      // console.log(`tx: ${tx}`)
      fromToTx.set(from, {
        ...fromToTx.get(from),
        [to]: { txs: nonceTxs, nonce }
      })
    }
  }
  return fromToTx;
}

// Returns UnsignedTransaction
function getUpdateAssociationTx(
  args, // : CurrenciesTransferArgs,
  info, // : BaseTxInfo,
  options //: OptionsWithMeta
) {
  return defineMethod(
    {
      ...info,
      method: {
        args,
        name: 'updateAssociation',
        pallet: 'authorMapping',
      },
    },
    options
  );
}

function signWith(
  pair, //: KeyringPair,
  signingPayload, //: string,
  options //: OptionsWithMeta
) {
  const { registry, metadataRpc } = options;
  // Important! The registry needs to be updated with latest metadata, so make
  // sure to run `registry.setMetadata(metadata)` before signing.
  registry.setMetadata(createMetadata(registry, metadataRpc));

  const { signature } = registry
    .createType("ExtrinsicPayload", signingPayload, {
      version: EXTRINSIC_VERSION,
    })
    .sign(pair);

  return signature;
}

async function providePolkadotApi() {
  let api
  const wsEndpoints = [
    'wss://wss.moonriver.moonbeam.network',
    'wss://moonriver.api.onfinality.io/public-ws',
    'wss://moonriver.kusama.elara.patract.io'
  ]
  let wsIndex = 0;
  while (true) {
    try {
      const provider = new WsProvider(wsEndpoints[wsIndex % wsEndpoints.length]);
      await new Promise((resolve, reject) => {
        provider.on('connected', () => resolve());
        provider.on('disconnected', () => reject());
      });      
      api = await ApiPromise.create({
        initWasm: false,
        provider,
        typesBundle: typesBundle,
      });
      return api
    
    } catch (e) {
      console.log(e)
      wsIndex++
    }
  }
}

async function scanSessions() {
  let params = { TableName: 'movrfailover-sessions' };
  let lastEvaluatedKey = 'dummy'; // string must not be empty
  const itemsAll = [];
  while (lastEvaluatedKey) {
    const data = await documentClient.scan(params).promise();
    itemsAll.push(...data.Items);
    lastEvaluatedKey = data.LastEvaluatedKey;
    if (lastEvaluatedKey) {
      params.ExclusiveStartKey = lastEvaluatedKey;
    }
  }
  return itemsAll;
}

async function updateSessionSignedTxs(nodeName, txs) {
  var params = {
    TableName: 'movrfailover-sessions',
    Key: {
      "nodeName": nodeName,
    },
    UpdateExpression: "set transactions = :t",
    ExpressionAttributeValues: {
      ":t": txs,
    }
  };
  await documentClient.update(params).promise();
}

async function kmsDecrypt(cipher) {
  var params = {
    CiphertextBlob: Buffer.from(cipher, 'base64'), // The encrypted data (ciphertext).
    KeyId: KMS_ARN
  };
  const { KeyId, Plaintext } = await kms.decrypt(params).promise();
  return Plaintext.toString('base64');
}

async function kmsEncrypt(text) {
  var params = {
    KeyId: KMS_ARN,
    Plaintext: Buffer.from(text, 'base64')
  };
  const { CiphertextBlob, KeyId } = await kms.encrypt(params).promise()
  return CiphertextBlob.toString('base64');
}

function encrypt(secret, text) {
  const hash = crypto.createHash('sha256').update(String(secret)).digest('base64').substr(0, 32);
  const iv = Buffer.alloc(16, 0); // Initialization vector.
  const cipher = crypto.createCipheriv('aes256', hash, iv);
  const encrypted = cipher.update(text, "utf8", "hex");
  return encrypted + cipher.final("hex")
}

function decrypt(secret, encrypted) {
  const hash = crypto.createHash('sha256').update(String(secret)).digest('base64').substr(0, 32);
  const iv = Buffer.alloc(16, 0); // Initialization vector.
  const decipher = crypto.createDecipheriv('aes256', hash, iv);
  return decipher.update(encrypted, "hex", "utf8") + decipher.final("utf8");
}