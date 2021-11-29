const AWS = require('aws-sdk')
const { ApiPromise, WsProvider } = require("@polkadot/api");
const { typesBundlePre900 } = require("moonbeam-types-bundle")
const crypto = require("crypto");

const badRequest = {
  "statusCode": 403, // Forbidden
  "headers": {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Credentials": "true"
  }
}

const isLambda = !!process.env.LAMBDA_TASK_ROOT;
// Create a Secrets Manager client
var options = {
  region: "eu-central-1"
}
var kms = new AWS.KMS(options);
const KMS_ARN = "arn:aws:kms:eu-central-1:XXXXXXXXXXXXXX:key/YOUR-KEY-XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"

/**
 * AWS Lambda + AWS Api Gateway
 * The lambda accepts a POST request that includes an encrypted signed raw transaction and a secret caller key
 * and executes that transaction on the network
 * 
 * @param {*} eventexample POST request: { "body": "{\"tx\":\"ENCRYPTETRANSACTIONDATA\",\"keyCaller\":\"CALLETSECRET\"}" }
 */
exports.handler = async (event) => {
  console.log(event)
  if (!event.body) {
    return badRequest
  }

  try {
    console.log('Extract data from request')
    const keyEnv = process.env.KEY_ENV
    const body = JSON.parse(event.body)
    let { tx, keyCaller } = body; // tx is encrypted signed transaction
    if (!tx || !keyCaller || !keyEnv) {
      return badRequest
    }

    console.log('Decrypting data')
    tx = await kmsDecrypt(tx) // shared key from kill account
    tx = decrypt(keyEnv, tx) // function's personal key stored in env
    tx = decrypt(keyCaller, tx) // telemetry watcher's secret

    console.log('Connecting to polkadot api')
    const polkadotApi = await providePolkadotApi();
    await polkadotApi.isReady;
    // Necessary hack to allow polkadotApi to finish its internal metadata loading
    // apiPromise.isReady unfortunately doesn't wait for those properly
    await new Promise((resolve) => {
      setTimeout(resolve, 100);
    });

    console.log('Sending transaction')
    const extrinsic = polkadotApi.createType("Extrinsic", tx);
    // execute the signed raw transaction
    await new Promise(async (res, reject) => {
      const unsub = await polkadotApi.rpc.author.submitAndWatchExtrinsic(extrinsic, (status) => {
        console.log("ok result", JSON.stringify(status.toHuman(), null, 2));
        if (status.isBroadcast) { // the tx has been broadcast to the given peers
          console.log('Transaction broadcasted')
        } else if (status.isInvalid || status.isDropped || status.isRetracted || status.isFinalityTimeout) {
          unsub();
          reject(status); //  throw error
        } else if (status.isInBlock) {
          console.log(`Transaction included in Block at blockHash ${status.asInBlock}`);
          unsub();
          res();
        } else if (status.isFinalized) {
          console.log(`Transaction finalized at blockHash ${status.asFinalized}`);
          // will not reach this (isInblock will fire first) but in any case...
          unsub();
          res();
        }
      });
    });

  } catch (e) {
    console.error(e)
    return badRequest
  }

  console.log('Success!')
  // if transaction is not include din a block within 29 seconds then the POST request times out
  // timing out does not imply failure; the caller will check if the reassociation completed via a separate call
  return {
    "statusCode": 200,
    "headers": {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true"
    },
    "body": JSON.stringify({ success: true }),
  };
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
        typesBundle: typesBundlePre900,
      });
      return api

    } catch (e) {
      console.log(e)
      wsIndex++
    }
  }
}

async function kmsDecrypt(cipher) {
  var params = {
    CiphertextBlob: Buffer.from(cipher, 'base64'), // The encrypted data (ciphertext).
    KeyId: KMS_ARN
  };
  const { KeyId, Plaintext } = await kms.decrypt(params).promise();
  return Plaintext.toString('base64');
}

function decrypt(secret, encrypted) {
  const hash = crypto.createHash('sha256').update(String(secret)).digest('base64').substr(0, 32);
  const iv = Buffer.alloc(16, 0); // Initialization vector.
  const decipher = crypto.createDecipheriv('aes256', hash, iv);
  return decipher.update(encrypted, "hex", "utf8") + decipher.final("utf8");
}

