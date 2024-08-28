
function validateEmail(email) {
    const re = /^(([^<>()\[\]\\.,;:\s@"]+(\.[^<>()\[\]\\.,;:\s@"]+)*)|(".+"))@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$/;
    return re.test(String(email).toLowerCase());
}

function signup() {
    const username = document.getElementById('username').value;
    const password = document.getElementById('password').value;
    const email = document.getElementById('email').value;
    if (!username || !password || !email) {
        alert('Please fill in all fields');
        return;
    }
    if (!validateEmail(email)) {
        alert('Please enter a valid email address');
        return;
    }
    const data = {username, password, email};
    fetch('/api/v1/user/register', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify(data),
    })
    .then(response => response.json())
    .then(data => {
        console.log('Success:', data);
        window.location.href = '/login';
    })
    .catch((error) => {
        console.error('Error:', error);
    });
}

async function main() {

    $("#signupform").submit(function(e) {
        e.preventDefault();
        signup();
    });
}

main();
