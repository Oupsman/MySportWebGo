
function signup() {
    const username = document.getElementById('username').value;
    const password = document.getElementById('password').value;
    const data = {username, password};
    fetch('/api/v1/user/login', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
    })
        .then(response => {
            console.log(response)
            if (response.status !== 200) {
                popupMessage('Invalid credentials', 'red');
            } else {
                console.log('Success:', data);
                window.location.href = '/';
         }})
        .catch((error) => {
            popupMessage('Invalid credentials', 'red');
            console.error('Error:', error);
        });
}

async function main() {
    $("#loginform").submit(function(e) {
        e.preventDefault();
        signup();
    });
}

main();
