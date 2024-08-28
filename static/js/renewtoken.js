let token = ""

function parseJwt (token) {
    var base64Url = token.split('.')[1];
    var base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    var jsonPayload = decodeURIComponent(atob(base64).split('').map(function(c) {
        return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));

    return JSON.parse(jsonPayload);
}

async function renewToken() {
    // Check every 500 milliseconds until the token is not empty
    while (token === "") {
        await new Promise(r => setTimeout(r, 500));
    }

    // Check token expiration
    console.log("Check if token is expired")
    let tokenData = parseJwt(token)
    let now = new Date();
    // Renew token every 5 minutes even if it's not expired
    let exp = new Date((tokenData.exp - 3300) * 1000)
    console.log("Expiration date:", exp.toString())
    if (now > exp) {
        // Call the refresh token function
        let newToken = await fetch('/api/v1/user/refreshtoken', {
            method: 'GET',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': 'Bearer ' + token,
            }}).then(function (response) {
            return response;

        }).then(function (response) {
            if (response.status === 200) {
                return response.json();
            }
            return ""
        }).catch(function (error) {
                console.error("Error: ", error)
            }
        )
        if(newToken !== "") {
            token = newToken.token
            postMessage(token)
        }
    }
    await new Promise(r => setTimeout(r, 60000));
    renewToken();
}
onmessage = function (e) {
    token = e.data
}

renewToken();