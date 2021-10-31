const { ApiPromise, WsProvider } = require("@polkadot/api");
const { typesBundle } = require("./moonbeam-types-bundle");

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
 */
exports.handler = async (event) => {
  console.log(event)
  if (!event.body) {
    return badRequest
  }

  let answer = {}
  try {
    console.log('Prepare Polkadot API')
    const { sessions } = JSON.parse(event.body)
    const polkadotApi = await providePolkadotApi();
    await polkadotApi.isReady;
    await new Promise((resolve) => {
      setTimeout(resolve, 100);
    });

    console.log('Get author mappings')
    let authorMappings = await polkadotApi.query.authorMapping.mappingWithDeposit.multi(sessions);
    console.log('authorMappings:', authorMappings)
    for(const g of authorMappings) {
      console.log(g.toHuman())
      console.log(typeof g.toHuman());
    }
    const accounts = authorMappings.map(c => c.toHuman() ? c.toHuman().account : undefined)
    console.log('accounts:', accounts);

    console.log('Get accounts')
    const accountsFiltered = accounts.filter(a => a)
    const accountDetails = await polkadotApi.query.system.account.multi(accountsFiltered)
    const nonces = accountDetails.map(a => +a.nonce)

    console.log('Format response')
    for (let i = 0; i < sessions.length; i++) {
      const account = accounts[i]
      answer[sessions[i]] = {
        active: !!account,
        account, // can be undefined
        nonce: account ? nonces[accountsFiltered.indexOf(account)] : undefined
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
        typesBundle: typesBundle,
      });
      return api
    
    } catch (e) {
      console.log(e)
      wsIndex++
    }
  }
}

