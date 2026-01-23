@0xb2a3f4c5d6e7f8a9;
# ZAP - Zero-Copy App Proto
# Cap'n Proto schema for high-performance agent communication

using Go = import "/go.capnp";
$Go.package("zap");
$Go.import("github.com/zap-protocol/zap-go");

# Core types

struct Timestamp {
  seconds @0 :Int64;
  nanos @1 :UInt32;
}

struct Metadata {
  entries @0 :List(Entry);

  struct Entry {
    key @0 :Text;
    value @1 :Text;
  }
}

# Tool definitions

struct Tool {
  name @0 :Text;
  description @1 :Text;
  schema @2 :Data;  # JSON Schema as bytes
  annotations @3 :Metadata;
}

struct ToolList {
  tools @0 :List(Tool);
}

struct ToolCall {
  id @0 :Text;
  name @1 :Text;
  args @2 :Data;  # JSON arguments as bytes
  metadata @3 :Metadata;
}

struct ToolResult {
  id @0 :Text;
  content @1 :Data;  # Result content as bytes
  error @2 :Text;
  metadata @3 :Metadata;
}

# Resource definitions

struct Resource {
  uri @0 :Text;
  name @1 :Text;
  description @2 :Text;
  mimeType @3 :Text;
  annotations @4 :Metadata;
}

struct ResourceList {
  resources @0 :List(Resource);
}

struct ResourceContent {
  uri @0 :Text;
  mimeType @1 :Text;
  content :union {
    text @2 :Text;
    blob @3 :Data;
  }
}

# Prompt definitions

struct Prompt {
  name @0 :Text;
  description @1 :Text;
  arguments @2 :List(Argument);

  struct Argument {
    name @0 :Text;
    description @1 :Text;
    required @2 :Bool;
  }
}

struct PromptList {
  prompts @0 :List(Prompt);
}

struct PromptMessage {
  role @0 :Role;
  content @1 :Content;

  enum Role {
    user @0;
    assistant @1;
    system @2;
  }

  struct Content {
    union {
      text @0 :Text;
      image @1 :ImageContent;
      resource @2 :ResourceContent;
    }
  }

  struct ImageContent {
    data @0 :Data;
    mimeType @1 :Text;
  }
}

# Server info

struct ServerInfo {
  name @0 :Text;
  version @1 :Text;
  capabilities @2 :Capabilities;

  struct Capabilities {
    tools @0 :Bool;
    resources @1 :Bool;
    prompts @2 :Bool;
    logging @3 :Bool;
  }
}

# Main ZAP interface

interface Zap {
  # Initialize connection
  init @0 (client :ClientInfo) -> (server :ServerInfo);

  # Tool operations
  listTools @1 () -> (tools :ToolList);
  callTool @2 (call :ToolCall) -> (result :ToolResult);

  # Resource operations
  listResources @3 () -> (resources :ResourceList);
  readResource @4 (uri :Text) -> (content :ResourceContent);
  subscribe @5 (uri :Text) -> (stream :ResourceStream);

  # Prompt operations
  listPrompts @6 () -> (prompts :PromptList);
  getPrompt @7 (name :Text, args :Metadata) -> (messages :List(PromptMessage));

  # Logging
  log @8 (level :LogLevel, message :Text, data :Data);

  enum LogLevel {
    debug @0;
    info @1;
    warn @2;
    error @3;
  }
}

struct ClientInfo {
  name @0 :Text;
  version @1 :Text;
}

interface ResourceStream {
  next @0 () -> (content :ResourceContent, done :Bool);
  cancel @1 () -> ();
}

# Gateway interface for MCP bridging

interface Gateway extends(Zap) {
  # Add MCP server
  addServer @0 (name :Text, url :Text, config :ServerConfig) -> (id :Text);

  # Remove MCP server
  removeServer @1 (id :Text) -> ();

  # List connected servers
  listServers @2 () -> (servers :List(ConnectedServer));

  # Get server status
  serverStatus @3 (id :Text) -> (status :ServerStatus);

  struct ServerConfig {
    transport @0 :Transport;
    auth @1 :Auth;
    timeout @2 :UInt32;  # milliseconds

    enum Transport {
      stdio @0;
      http @1;
      websocket @2;
      zap @3;
      unix @4;
    }

    struct Auth {
      union {
        none @0 :Void;
        bearer @1 :Text;
        basic @2 :BasicAuth;
      }

      struct BasicAuth {
        username @0 :Text;
        password @1 :Text;
      }
    }
  }

  struct ConnectedServer {
    id @0 :Text;
    name @1 :Text;
    url @2 :Text;
    status @3 :ServerStatus;
    tools @4 :UInt32;
    resources @5 :UInt32;
  }

  enum ServerStatus {
    connecting @0;
    connected @1;
    disconnected @2;
    error @3;
  }
}

# Coordination interface for distributed agents

interface Coordinator {
  # Register agent
  register @0 (agent :AgentInfo) -> (id :Text, gateway :Gateway);

  # Heartbeat
  heartbeat @1 (id :Text) -> (ok :Bool);

  # Discover agents
  discover @2 (filter :AgentFilter) -> (agents :List(AgentInfo));

  struct AgentInfo {
    id @0 :Text;
    name @1 :Text;
    capabilities @2 :List(Text);
    metadata @3 :Metadata;
  }

  struct AgentFilter {
    capabilities @0 :List(Text);
    metadata @1 :Metadata;
  }
}

# Post-Quantum Cryptography Types
# Implements NIST FIPS 203 (ML-KEM) and FIPS 204 (ML-DSA)

struct MLKEMPublicKey {
  # ML-KEM-768 public key (1184 bytes)
  data @0 :Data;
}

struct MLKEMCiphertext {
  # ML-KEM-768 ciphertext (1088 bytes)
  data @0 :Data;
}

struct MLDSAPublicKey {
  # ML-DSA-65 public key (1952 bytes)
  data @0 :Data;
}

struct MLDSASignature {
  # ML-DSA-65 detached signature (3293 bytes)
  data @0 :Data;
}

struct X25519PublicKey {
  # X25519 public key (32 bytes)
  data @0 :Data;
}

# Hybrid X25519 + ML-KEM handshake for post-quantum key exchange
struct PQHandshake {
  # Initiator's X25519 ephemeral public key
  x25519PublicKey @0 :Data;

  # Initiator's ML-KEM-768 public key
  mlkemPublicKey @1 :MLKEMPublicKey;

  # Responder's ML-KEM ciphertext (encapsulated shared secret)
  mlkemCiphertext @2 :MLKEMCiphertext;

  # Optional: signature over handshake transcript for authentication
  signature @3 :MLDSASignature;
}

# Handshake message for initiating a PQ-secure connection
struct PQHandshakeInit {
  # Client's X25519 ephemeral public key (32 bytes)
  x25519PublicKey @0 :Data;

  # Client's ML-KEM-768 public key for key encapsulation
  mlkemPublicKey @1 :MLKEMPublicKey;

  # Optional: Client's ML-DSA identity public key for authentication
  identityKey @2 :MLDSAPublicKey;

  # Optional: signature over (x25519PublicKey || mlkemPublicKey)
  identitySignature @3 :MLDSASignature;

  # Random nonce to prevent replay attacks
  nonce @4 :Data;

  # Protocol version
  version @5 :UInt16;
}

# Handshake response from server
struct PQHandshakeResponse {
  # Server's X25519 ephemeral public key (32 bytes)
  x25519PublicKey @0 :Data;

  # ML-KEM ciphertext containing encapsulated shared secret
  mlkemCiphertext @1 :MLKEMCiphertext;

  # Optional: Server's ML-DSA identity public key
  identityKey @2 :MLDSAPublicKey;

  # Optional: signature over (clientNonce || x25519PublicKey || mlkemCiphertext)
  identitySignature @3 :MLDSASignature;

  # Echo client nonce to bind response to request
  clientNonce @4 :Data;

  # Server nonce for session binding
  serverNonce @5 :Data;
}

# Authenticated channel after PQ handshake
struct PQChannelMessage {
  # Sequence number for replay protection
  sequence @0 :UInt64;

  # AEAD-encrypted payload (using derived session key)
  ciphertext @1 :Data;

  # AEAD authentication tag
  tag @2 :Data;

  # Associated data (plaintext, authenticated but not encrypted)
  associatedData @3 :Data;
}

# Key rotation message for long-lived sessions
struct PQKeyRotation {
  # New ML-KEM public key for next epoch
  newMlkemPublicKey @0 :MLKEMPublicKey;

  # ML-KEM ciphertext for the new key
  mlkemCiphertext @1 :MLKEMCiphertext;

  # Signature over (epoch || newMlkemPublicKey) with identity key
  signature @2 :MLDSASignature;

  # Epoch number (monotonically increasing)
  epoch @3 :UInt64;
}

# =============================================================================
# Ringtail Consensus Types
# Threshold lattice-based signing protocol for post-quantum security
# =============================================================================

# Ring polynomial coefficients mod Q
struct Poly {
  # Coefficients in Z_Q[X]/(X^phi + 1), phi=256
  coeffs @0 :List(UInt64);
}

struct PolyVector {
  polys @0 :List(Poly);
}

struct PolyMatrix {
  rows @0 :List(PolyVector);
}

# Ringtail protocol parameters
struct RingtailParams {
  m @0 :UInt16 = 8;                # Matrix rows
  n @1 :UInt16 = 7;                # Matrix columns
  dbar @2 :UInt16 = 48;            # Commitment dimension
  kappa @3 :UInt16 = 23;           # Challenge weight (Hamming weight)
  phi @4 :UInt16 = 256;            # Ring dimension (2^8)
  keySize @5 :UInt16 = 32;         # Key size in bytes (256 bits)
  q @6 :UInt64 = 0x1000000004A01;  # 48-bit NTT-friendly prime modulus
  xi @7 :UInt32 = 30;              # Rounding parameter
  nu @8 :UInt32 = 29;              # Rounding parameter
}

# Party information for threshold signing
struct RingtailPartyInfo {
  partyId @0 :UInt32;              # This party's ID (0-indexed)
  totalParties @1 :UInt32;         # Total number of parties (K)
  threshold @2 :UInt32;            # Signing threshold (t-of-K)
  address @3 :Text;                # Network address
  publicKey @4 :Data;              # Party's public key share
}

# MAC for authenticating round 1 commitments
struct RingtailMac {
  senderId @0 :UInt32;
  recipientId @1 :UInt32;
  value @2 :Data;                  # 32-byte BLAKE3 MAC
}

# Round 1 output: commitment D_i and MACs
struct RingtailRound1Output {
  partyId @0 :UInt32;
  sessionId @1 :UInt64;
  dMatrix @2 :PolyMatrix;          # M x (Dbar+1) commitment matrix D_i
  macs @3 :List(RingtailMac);      # MACs for other participating parties
  timestamp @4 :Timestamp;
}

# Round 2 output: response share z_i
struct RingtailRound2Output {
  partyId @0 :UInt32;
  sessionId @1 :UInt64;
  zShare @2 :PolyVector;           # N-dimensional response vector z_i
  timestamp @3 :Timestamp;
}

# Final Ringtail threshold signature (c, z, Delta)
struct RingtailSignature {
  c @0 :Poly;                      # Challenge polynomial (sparse, Hamming weight kappa)
  z @1 :PolyVector;                # Aggregated response vector (N-dimensional)
  delta @2 :PolyVector;            # Correction term (M-dimensional)
  signers @3 :List(UInt32);        # IDs of parties that contributed
  sessionId @4 :UInt64;            # Signing session identifier
}

# Request to initiate threshold signing
struct RingtailSignRequest {
  message @0 :Data;                # Message to sign
  sessionId @1 :UInt64;            # Unique session identifier
  participants @2 :List(UInt32);   # Party IDs participating in this signing
  timeoutMs @3 :UInt32;            # Timeout in milliseconds
}

# Response from signing operation
struct RingtailSignResponse {
  union {
    signature @0 :RingtailSignature;  # Completed signature
    error @1 :Text;                   # Error message
    progress @2 :RingtailSignProgress; # Still in progress
  }
}

# Progress update during threshold signing
struct RingtailSignProgress {
  sessionId @0 :UInt64;
  currentRound @1 :UInt8;          # 1 or 2
  completedParties @2 :List(UInt32);
  pendingParties @3 :List(UInt32);
  estimatedTimeMs @4 :UInt32;
}

# Request to verify a Ringtail signature
struct RingtailVerifyRequest {
  message @0 :Data;
  signature @1 :RingtailSignature;
  publicKey @2 :PolyVector;        # Public key b_tilde
  publicMatrix @3 :PolyMatrix;     # Public matrix A
}

# Response from verification
struct RingtailVerifyResponse {
  valid @0 :Bool;
  error @1 :Text;
}

# Peer-to-peer message for distributed signing
struct RingtailPeerMessage {
  from @0 :UInt32;                 # Sender party ID
  to @1 :UInt32;                   # Recipient (0 = broadcast)
  sessionId @2 :UInt64;
  timestamp @3 :Timestamp;

  union {
    round1 @4 :RingtailRound1Output;
    round2 @5 :RingtailRound2Output;
    signRequest @6 :RingtailSignRequest;
    signResponse @7 :RingtailSignResponse;
    heartbeat @8 :RingtailHeartbeat;
    abort @9 :RingtailAbort;
  }
}

# Heartbeat for liveness checking
struct RingtailHeartbeat {
  partyId @0 :UInt32;
  status @1 :RingtailPartyStatus;
  currentSession @2 :UInt64;
  load @3 :Float32;                # Current load (0.0-1.0)
}

enum RingtailPartyStatus {
  idle @0;
  signingRound1 @1;
  signingRound2 @2;
  combining @3;
  busy @4;
  offline @5;
}

# Abort message to cancel a signing session
struct RingtailAbort {
  sessionId @0 :UInt64;
  reason @1 :Text;
  partyId @2 :UInt32;
}

# =============================================================================
# Agent Consensus Types
# Simplified voting-based consensus for AI agent response aggregation
# =============================================================================

# Query identifier (32-byte hash)
struct AgentQueryId {
  hash @0 :Data;                   # BLAKE3 hash of query content
}

# State of a consensus query
struct AgentQueryState {
  queryId @0 :AgentQueryId;
  query @1 :Text;                  # Original query content
  responses @2 :List(AgentResponseEntry);
  votes @3 :List(AgentResponseVote);
  finalized @4 :Bool;
  result @5 :Text;                 # Final consensus result (if finalized)
  createdAt @6 :Timestamp;
}

# Response from an agent
struct AgentResponseEntry {
  agentId @0 :Text;
  response @1 :Text;
  timestamp @2 :Timestamp;
  signature @3 :Data;              # Optional signature over response
  confidence @4 :Float32;          # Agent's confidence (0.0-1.0)
}

# Vote tally for a response
struct AgentResponseVote {
  responseHash @0 :Data;           # BLAKE3 hash of response content
  voteCount @1 :UInt32;
  voters @2 :List(Text);           # Agent IDs that voted for this response
}

# Configuration for agent consensus
struct AgentConsensusConfig {
  threshold @0 :Float64;           # Required vote fraction (0.0-1.0)
  minResponses @1 :UInt32;         # Minimum responses before consensus check
  timeoutSecs @2 :UInt32;          # Query timeout in seconds
  requireSignatures @3 :Bool;      # Whether agent signatures are required
}

# Request to submit a new query
struct AgentSubmitQueryRequest {
  query @0 :Text;
  config @1 :AgentConsensusConfig;
}

# Response from submitting a query
struct AgentSubmitQueryResponse {
  queryId @0 :AgentQueryId;
}

# Request to submit an agent's response
struct AgentSubmitResponseRequest {
  queryId @0 :AgentQueryId;
  agentId @1 :Text;
  response @2 :Text;
  signature @3 :Data;              # Optional signature
  confidence @4 :Float32;
}

# Result of submitting a response
struct AgentSubmitResponseResult {
  union {
    success @0 :Void;
    error @1 :Text;
    duplicate @2 :Void;            # Agent already submitted
  }
}

# Request to check for consensus
struct AgentTryConsensusRequest {
  queryId @0 :AgentQueryId;
}

# Result of consensus check
struct AgentTryConsensusResponse {
  union {
    result @0 :Text;               # Consensus reached - final answer
    pending @1 :AgentQueryState;   # Still collecting responses
    noConsensus @2 :AgentQueryState; # Min responses met but no consensus
    error @3 :Text;
  }
}

# =============================================================================
# RPC Interfaces for Consensus
# =============================================================================

# Threshold signing party interface
interface RingtailParty {
  # Get party information
  getInfo @0 () -> (info :RingtailPartyInfo);

  # Execute Round 1 of signing protocol
  signRound1 @1 (request :RingtailSignRequest) -> (output :RingtailRound1Output);

  # Execute Round 2 of signing protocol
  signRound2 @2 (
    request :RingtailSignRequest,
    round1Outputs :List(RingtailRound1Output)
  ) -> (output :RingtailRound2Output);

  # Finalize signature (combiner only)
  finalize @3 (
    request :RingtailSignRequest,
    round2Outputs :List(RingtailRound2Output)
  ) -> (response :RingtailSignResponse);

  # Verify a signature
  verify @4 (request :RingtailVerifyRequest) -> (response :RingtailVerifyResponse);
}

# Agent consensus voting service
interface AgentConsensusService {
  # Submit a new query for consensus
  submitQuery @0 (request :AgentSubmitQueryRequest) -> (response :AgentSubmitQueryResponse);

  # Submit an agent's response to a query
  submitResponse @1 (request :AgentSubmitResponseRequest) -> (result :AgentSubmitResponseResult);

  # Try to reach consensus on a query
  tryConsensus @2 (request :AgentTryConsensusRequest) -> (response :AgentTryConsensusResponse);

  # Get current query state
  getQuery @3 (queryId :AgentQueryId) -> (state :AgentQueryState);

  # Clean up expired queries
  cleanup @4 () -> ();

  # Get number of active queries
  activeQueries @5 () -> (count :UInt32);
}

# Coordinator for distributed signing sessions
interface RingtailCoordinator {
  # Initialize a new signing session
  initSession @0 (
    message :Data,
    participants :List(UInt32)
  ) -> (sessionId :UInt64);

  # Collect Round 1 outputs from all parties
  collectRound1 @1 (sessionId :UInt64) -> (outputs :List(RingtailRound1Output));

  # Collect Round 2 outputs from all parties
  collectRound2 @2 (sessionId :UInt64) -> (outputs :List(RingtailRound2Output));

  # Get final signature for a session
  getSignature @3 (sessionId :UInt64) -> (response :RingtailSignResponse);

  # Cancel a signing session
  cancelSession @4 (sessionId :UInt64) -> ();

  # List active sessions
  listSessions @5 () -> (sessions :List(UInt64));
}

# =============================================================================
# W3C Decentralized Identifier (DID) Types
# Implements W3C DID Core 1.0 specification
# =============================================================================

# DID Method enum
enum DidMethod {
  lux @0;     # Lux blockchain-anchored DID
  key @1;     # Self-certifying DID from cryptographic key
  web @2;     # DNS-based DID
}

# Decentralized Identifier
struct Did {
  method @0 :DidMethod;
  id @1 :Text;           # Method-specific identifier (e.g., z6Mk...)
}

# Verification method type
enum VerificationMethodType {
  jsonWebKey2020 @0;
  multikey @1;
  mlDsa65VerificationKey2024 @2;
}

# Verification method (public key) in DID Document
struct VerificationMethod {
  id @0 :Text;           # e.g., "did:lux:z6Mk...#keys-1"
  type @1 :VerificationMethodType;
  controller @2 :Text;   # Controller DID
  publicKeyMultibase @3 :Text;  # Multibase-encoded public key
  publicKeyJwk @4 :Data;        # JSON Web Key (optional, as JSON bytes)
  blockchainAccountId @5 :Text; # Blockchain account ID (for Lux DIDs)
}

# Service type
enum ServiceType {
  zapAgent @0;           # ZAP Agent service
  didCommMessaging @1;   # DID Communication
  linkedDomains @2;      # Linked Domains
  credentialRegistry @3; # Credential Registry
}

# Service endpoint
struct ServiceEndpoint {
  union {
    uri @0 :Text;        # Single URI endpoint
    uris @1 :List(Text); # Multiple URI endpoints
    structured @2 :StructuredEndpoint;
  }

  struct StructuredEndpoint {
    uri @0 :Text;
    accept @1 :List(Text);
    routingKeys @2 :List(Text);
  }
}

# Service in DID Document
struct Service {
  id @0 :Text;           # e.g., "did:lux:z6Mk...#zap-agent"
  type @1 :ServiceType;
  serviceEndpoint @2 :ServiceEndpoint;
}

# W3C DID Document
struct DidDocument {
  context @0 :List(Text);  # JSON-LD context
  id @1 :Text;             # DID subject
  controller @2 :Text;     # Optional controller DID

  # Verification methods (public keys)
  verificationMethod @3 :List(VerificationMethod);

  # Verification relationships (references to verification method IDs)
  authentication @4 :List(Text);
  assertionMethod @5 :List(Text);
  keyAgreement @6 :List(Text);
  capabilityInvocation @7 :List(Text);
  capabilityDelegation @8 :List(Text);

  # Service endpoints
  service @9 :List(Service);
}

# Node identity for ZAP network participation
struct NodeIdentity {
  did @0 :Did;
  publicKey @1 :Data;    # ML-DSA-65 public key (1952 bytes)
  stake @2 :UInt64;      # Staked amount (optional, 0 if none)
  stakeRegistry @3 :Text; # Stake registry reference
}

# Stake registry entry
struct StakeEntry {
  did @0 :Did;
  amount @1 :UInt64;
  timestamp @2 :Timestamp;
}

# Request to resolve a DID to its document
struct DidResolveRequest {
  did @0 :Text;          # Full DID URI string
}

# Response from DID resolution
struct DidResolveResponse {
  union {
    document @0 :DidDocument;
    error @1 :Text;
    notFound @2 :Void;
  }
}

# Request to create/register a new DID
struct DidCreateRequest {
  method @0 :DidMethod;
  publicKey @1 :Data;    # ML-DSA-65 public key
  services @2 :List(Service); # Optional initial services
}

# Response from DID creation
struct DidCreateResponse {
  union {
    did @0 :Did;
    error @1 :Text;
  }
}

# DID Registry interface for resolution and management
interface DidRegistry {
  # Resolve a DID to its document
  resolve @0 (request :DidResolveRequest) -> (response :DidResolveResponse);

  # Create/register a new DID
  create @1 (request :DidCreateRequest) -> (response :DidCreateResponse);

  # Update a DID document (requires authentication)
  update @2 (did :Text, document :DidDocument, signature :Data) -> (success :Bool, error :Text);

  # Deactivate a DID (requires authentication)
  deactivate @3 (did :Text, signature :Data) -> (success :Bool, error :Text);

  # Get stake for a DID
  getStake @4 (did :Text) -> (amount :UInt64);

  # Set stake for a DID (requires authentication)
  setStake @5 (did :Text, amount :UInt64, signature :Data) -> (success :Bool, error :Text);
}
