const { ApiPromise, WsProvider } = require("@polkadot/api");
// const { typesBundle } = require("./moonbeam-types-bundle");
const { typesBundlePre900 } = require("moonbeam-types-bundle")

const badRequest = {
  "statusCode": 403, // Forbidden
  "headers": {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Credentials": "true"
  }
}

const isLambda = !!process.env.LAMBDA_TASK_ROOT;

/**
 * AWS Lambda + AWS Api Gateway
 * The lambda accepts a POST request that includes an array of session keys
 * and returns which of these session keys are currently associated and the nonces of the respective accounts
 * Alternaively, the function receives an array of accounts and returns their nonces
 * 
 * sample input: '{"sessions":["0xf81d1f76cf102c71fb51338e8aed4a00cbe564ce5cdc01bb2c1a9c09b80d9f06","0x064e1443535e1c427e6c91a1da5e50571ea1b80c509ea96fb0b30ac61660a712"]}'
 * sample response: "{\"0xf81d1f76cf102c71fb51338e8aed4a00cbe564ce5cdc01bb2c1a9c09b80d9f06\":{\"active\":true,\"account\":\"0x1980E75f1b1cdAAe3b2f79664C7cb83b86A3D404\",\"nonce\":83},\"0x064e1443535e1c427e6c91a1da5e50571ea1b80c509ea96fb0b30ac61660a712\":{\"active\":false}}"
 */
exports.handler = async (event) => {
  console.log(event)
  if (!event.body) {
    return badRequest
  }

  let answer = {}
  try {
    console.log('Prepare Polkadot API')
    let { sessions, accounts } = JSON.parse(event.body)
    const polkadotApi = await providePolkadotApi();
    await polkadotApi.isReady;
    await new Promise((resolve) => {
      setTimeout(resolve, 100);
    });

    if (sessions) {
      console.log('Get author mappings')
      let authorMappings = await polkadotApi.query.authorMapping.mappingWithDeposit.multi(sessions);
      console.log('authorMappings:', authorMappings)
      for (const g of authorMappings) {
        console.log(g.toHuman())
        console.log(typeof g.toHuman());
      }
      accounts = authorMappings.map(c => c.toHuman() ? c.toHuman().account : undefined)
    }
    console.log('accounts:', accounts);

    console.log('Get accounts')
    const accountsFiltered = accounts.filter(a => a)
    const accountDetails = await polkadotApi.query.system.account.multi(accountsFiltered)
    const nonces = accountDetails.map(a => +a.nonce)

    console.log('Format response')
    if (sessions) {
      for (let i = 0; i < sessions.length; i++) {
        const account = accounts[i]
        answer[sessions[i]] = {
          active: !!account,
          account, // can be undefined
          nonce: account ? nonces[accountsFiltered.indexOf(account)] : undefined
        }
      }
    } else {
      for (let i = 0; i < accounts.length; i++) {
        answer[accounts[i]] = nonces[i]
      }
    }
  } catch (e) {
    console.log(e)
    return badRequest;
  }

  return {
    "statusCode": 200,
    "headers": {
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Credentials": "true"
    },
    "body": JSON.stringify(answer),
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

