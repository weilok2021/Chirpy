# Understanding JWT — A Walkthrough

A personal study note covering how JWT (JSON Web Tokens) actually work, with the Chirpy project as the running example.

---

## 1. The Big Picture

```
┌────────┐                                ┌────────┐
│ Client │                                │ Server │
└────┬───┘                                └────┬───┘
     │                                         │
     │  POST /api/login {email, password}      │
     ├────────────────────────────────────────>│
     │                                         │ verify password
     │                                         │ create JWT signed with SECRET
     │                                         │
     │  200 OK { token: "eyJ..." }             │
     │<────────────────────────────────────────┤
     │                                         │
     │  GET /api/chirps                        │
     │  Authorization: Bearer eyJ...           │
     ├────────────────────────────────────────>│
     │                                         │ verify signature with SECRET
     │                                         │ if valid → read user_id from payload
     │                                         │ if invalid → 401
     │  200 OK [chirps...]                     │
     │<────────────────────────────────────────┤
```

**Key idea:** The server is *stateless*. It does not store a "logged-in users" list. The token itself carries the proof of who the user is, and the signature is what makes it trustworthy.

---

## 2. What a JWT Actually Is

A JWT is a single string with **three parts** separated by dots:

```
eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLTEyMyIsImV4cCI6MTcwMDAwMDAwMH0.kF8z7r3N...
└─────── header ──────┘ └────────────── payload ──────────────┘ └─ signature ─┘
```

### Part 1 — Header (base64-encoded JSON)

Decodes to:
```json
{ "alg": "HS256", "typ": "JWT" }
```
Says "this token was signed using HMAC-SHA256."

### Part 2 — Payload (base64-encoded JSON)

Decodes to something like:
```json
{
  "iss": "chirpy",
  "sub": "550e8400-e29b-41d4-a716-446655440000",
  "iat": 1700000000,
  "exp": 1700003600
}
```
This is where you put **claims** — facts about the user. In Chirpy, `sub` (subject) is the user's UUID.

> **Important:** This is *base64-encoded*, NOT encrypted. Anyone can read it. Never put a password, credit card number, or any secret in here.

### Part 3 — Signature

Computed as:
```
signature = HMAC-SHA256(
    base64(header) + "." + base64(payload),
    SECRET
)
```
Then base64-encoded and appended.

This is the *only* part that requires the secret. It's also the only part that provides security.

---

## 3. Why This Design Works

The signature is a **cryptographic proof** that:

1. The token was created by someone who knows `SECRET`.
2. Nobody has tampered with the header or payload since it was signed.

**If an attacker tries to modify the payload** (e.g. change `sub` to a different user's UUID):
- They can decode the payload, change the JSON, re-base64-encode it.
- But they can't recompute the signature without the secret.
- When the new payload is hashed with the *real* secret on the server side, it produces a different signature than the one on the token.
- Verification fails → 401.

**If an attacker forges a token from scratch:**
- Same problem. Without the secret, they cannot produce a signature that the server's verification step will accept.

So the secret is the entire foundation of the security. Leak it and the whole system breaks — anyone can impersonate any user.

---

## 4. The Validation Step (Where You Were Confused)

When the client sends the token back on a protected endpoint, the server does this:

### Step 1: Split the token
```
header_b64 . payload_b64 . received_signature
```

### Step 2: Recompute the signature
```go
expected_signature = HMAC-SHA256(header_b64 + "." + payload_b64, SECRET)
```
Note: this is computed from the **received** header and payload — *not* anything stored on the server. There is nothing to "look up."

### Step 3: Compare
```
if expected_signature == received_signature {
    // Valid — payload is trustworthy
} else {
    // Invalid — reject (401)
}
```

### Step 4: Check expiry
Even with a valid signature, look at the `exp` claim. If `exp < now`, reject as expired.

### Step 5: Read claims
Now that the token is verified, parse `sub` to know which user this is.

**Crucial point:** The server stores NOTHING per-token. There is no "issued tokens" table. Validation is pure math: take the input, run HMAC with the secret, compare. That is what makes JWT *stateless*.

---

## 5. Mapping to Your Go Code

### Issuing a token (login endpoint)

```go
func MakeJWT(userID uuid.UUID, tokenSecret string, expiresIn time.Duration) (string, error) {
    claims := jwt.RegisteredClaims{
        Issuer:    "chirpy",
        IssuedAt:  jwt.NewNumericDate(time.Now()),
        ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
        Subject:   userID.String(),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(tokenSecret))
}
```

What's happening:
- Build the claims object (the payload).
- `jwt.NewWithClaims` builds the unsigned token (header + payload).
- `SignedString` runs HMAC-SHA256 over `header.payload` using the secret, base64-encodes the result, and appends it as the third segment.
- Returns the full `header.payload.signature` string.

### Validating a token (middleware or handler)

```go
func ValidateJWT(tokenString, tokenSecret string) (uuid.UUID, error) {
    claims := &jwt.RegisteredClaims{}
    token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
        return []byte(tokenSecret), nil
    })
    if err != nil {
        return uuid.Nil, err
    }
    if !token.Valid {
        return uuid.Nil, errors.New("invalid token")
    }
    return uuid.Parse(claims.Subject)
}
```

What `ParseWithClaims` does internally:
1. Splits the token on `.` into three parts.
2. Base64-decodes the header to figure out which algorithm to expect (HS256).
3. Calls your **keyfunc** to get the secret. (This callback design lets advanced apps choose different keys based on the token's `kid` header.)
4. Recomputes the signature over `header.payload` using that secret.
5. Compares with the signature segment.
6. Decodes the payload into your `claims` struct (writing through the pointer).
7. Checks `exp` automatically and sets `token.Valid` accordingly.

After `ParseWithClaims` succeeds and `token.Valid` is true, `claims.Subject` is trustworthy — you can convert it back to a UUID and use it as the authenticated user ID.

---

## 6. The Full Lifecycle, End to End

Imagine a user logging in and posting a chirp:

### Login (POST /api/login)
1. Client sends `{ "email": "...", "password": "..." }`.
2. Server looks up the user, runs `bcrypt.CompareHashAndPassword`.
3. If the password matches, server calls `MakeJWT(user.ID, SECRET, 1*time.Hour)`.
4. Server returns `{ "token": "eyJ..." }`. The token contains the user ID and an expiry one hour from now.
5. Client stores the token (memory, localStorage, secure cookie — depends on the client).

### Authenticated request (POST /api/chirps)
1. Client sends:
   ```
   POST /api/chirps
   Authorization: Bearer eyJ...
   { "body": "Hello world" }
   ```
2. Server's auth middleware extracts the token from the `Authorization` header.
3. Server calls `ValidateJWT(token, SECRET)`:
   - Recomputes the signature with `SECRET`.
   - Compares with the signature on the token.
   - Checks `exp` is in the future.
   - If all good, returns the user UUID from `sub`.
4. Handler uses that UUID as the chirp's `user_id` and inserts into the database.

### After the token expires
1. Client sends a request with the now-expired token.
2. `ValidateJWT` returns an error because `exp < now`.
3. Server returns 401.
4. Client must call `/api/login` again (or use a refresh token, which is a topic for later).

---

## 7. Common Mistakes & Gotchas

- **"I'll just store the password in the JWT."** No. The payload is readable by anyone with the token. Only put non-sensitive identifiers (user ID, role, etc.).
- **"I'll store JWTs in a database to validate them."** Don't — that defeats the whole point. JWT validation is local math. If you need server-side revocation, use sessions instead, or layer a deny-list on top.
- **Logging out doesn't really invalidate a JWT.** Since the server is stateless, "logout" usually means "client throws the token away." If you truly need server-enforced logout, you need short token lifetimes plus a refresh-token rotation scheme, or a deny-list.
- **Don't reuse the same secret across environments.** Production and dev must have different secrets, and the secret should not be in source control.
- **`sub` is a string.** The `RegisteredClaims` standard requires it. Convert your UUID to/from string at the boundary.
- **Clock skew.** If the issuing server and validating server have clocks more than a few seconds apart, tokens may seem expired or not-yet-valid. The library has tolerance options (`jwt.WithLeeway`).
- **Algorithm confusion attacks.** Older JWT libraries had a bug where you could send a token with `alg: none` and bypass signature checks. `golang-jwt/jwt/v5` rejects this by default. Don't disable that protection.

---

## 8. The One-Sentence Summary

> A JWT is a self-contained, signed claim. Validation means recomputing its signature with your secret and checking the signature matches — no database lookup, no session table, just a hash comparison. If the math works out, the claims inside are trustworthy.
