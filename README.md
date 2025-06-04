# SSHRoulette

SSHRoulette is a simple SSH-based anonymous chat server that pairs users into random chat sessions over SSH. Users connect via SSH (with or without a password), receive a randomized anonymous username, and get matched instantly with another random user currently online.

## Features

- Connect via SSH on a custom port (default: 2255)
- No account or registration required
- Random anonymous usernames assigned on connect (e.g. `anon4816`)
- Paired 1-on-1 with a random user for a live text chat session
- Commands to control your chat session:
  - `/next` --- find a new random chat partner without disconnecting
  - `/quit` --- end the chat session and disconnect
- Automatically rematches when your chat partner leaves

## Example Session

```bash
$ ssh localhost -p 2255
Welcome to SSH Roulette! Your name is anon4816
Waiting to be matched with another user...
Paired with anon8338! Type /next to find someone else or /quit to disconnect.
[anon8338] Hello
testing
hello
/next
Searching for a new match...
Paired with anon5320! Type /next for another match or /quit to disconnect.
Hi there
[anon5320] Hello how are you
[anon5320] good
anon5320 has left the chat. Searching for a new match...

```

Usage
-----

Users connect via SSH:

```
ssh yourserver.com -p 2255

```

They will receive a random anonymous username and get paired automatically with another user. Chat in real-time and use `/next` or `/quit` commands as needed.

Commands
--------

-   `/next` --- Leave your current chat partner and get paired with a new random user.

-   `/quit` --- Exit the chat session and disconnect from the server.

Security & Privacy
------------------

-   No user registration or personal data is stored.

-   Usernames are randomized per session and anonymous.

-   The server supports both password and password-less configuration.


Enjoy anonymous SSH chatting with SSHRoulette!\
Feel free to reach out for questions or support.
