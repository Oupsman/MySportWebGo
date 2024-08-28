
function popupMessage(message, color) {
    const messagePopup = $("#message");
    messagePopup.html(message);
    messagePopup.css("background-color", color);
    messagePopup.css("color", "white");
    messagePopup.show();
    messagePopup.fadeOut(5000);
}