export const generateRandomName = () => {
    const characters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
    let result = '';
    for (let i = 0; i < 5; i++) {
        result += characters.charAt(Math.floor(Math.random() * characters.length));
    }
    return result;
};

for (let i = 0; i < 500000; i++) {
    const name = generateRandomName();

    const response = await fetch("http://localhost:3000/user", {
        method: "POST",
        body: JSON.stringify({ username: name }),
        headers: { "Content-Type": "application/json" },
    });

    const body = await response.text();
    console.log(body);
}