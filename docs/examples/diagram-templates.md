# Mermaid Diagram Templates

Collection of Mermaid diagram templates compatible with merm8 linting.

## Flowchart Templates

### Basic Linear Flow

```mermaid
graph TD
    A[Start] --> B[Process]
    B --> C[Decision]
    C -->|Yes| D[Action TRUE]
    C -->|No| E[Action FALSE]
    D --> F[End]
    E --> F
```

### Parallel Processes

```mermaid
graph TD
    Start[Initialize] --> Process1[Task A]
    Start --> Process2[Task B]
    Start --> Process3[Task C]
    Process1 --> Merge[Collect Results]
    Process2 --> Merge
    Process3 --> Merge
    Merge --> End[Complete]
```

### Hierarchical System

```mermaid
graph TD
    API[API Gateway]
    AuthSvc[Auth Service]
    UserSvc[User Service]
    OrderSvc[Order Service]
    DB[(Database)]
    Cache[(Cache)]

    API --> AuthSvc
    API --> UserSvc
    API --> OrderSvc
    UserSvc --> DB
    OrderSvc --> DB
    UserSvc --> Cache
    OrderSvc --> Cache
```

### Error Handling Pipeline

```mermaid
graph TD
    Request[Incoming Request]
    Validate{Valid?}
    Process[Process Data]
    CheckError{Error?}
    Retry[Retry Logic]
    Success[Return Success]
    Error[Return Error]

    Request --> Validate
    Validate -->|No| Error
    Validate -->|Yes| Process
    Process --> CheckError
    CheckError -->|Yes| Retry
    CheckError -->|No| Success
    Retry --> Process
```

### Microservices Architecture

```mermaid
graph TD
    subgraph Frontend[Frontend Services]
        Web[Web App]
        Mobile[Mobile App]
    end

    subgraph Backend[Backend Services]
        API[API Server]
        Auth[Auth Service]
        User[User Service]
        Product[Product Service]
    end

    subgraph Data[Data Layer]
        UserDB[User DB]
        ProductDB[Product DB]
        Cache[Redis Cache]
    end

    Web --> API
    Mobile --> API
    API --> Auth
    API --> User
    API --> Product
    User --> UserDB
    Product --> ProductDB
    User --> Cache
    Product --> Cache
    Auth --> UserDB
```

### State Machine

```mermaid
graph TD
    Start[(Start)]
    Idle[Idle State]
    Active[Active State]
    Processing[Processing State]
    Error[Error State]
    Done[(Done)]

    Start --> Idle
    Idle -->|Trigger| Active
    Active -->|Process| Processing
    Processing -->|Success| Idle
    Processing -->|Failure| Error
    Error -->|Retry| Processing
    Error -->|Abandon| Done
    Idle -->|Exit| Done
```

## Sequence Diagram Templates (Parser-supported, lint rules not yet enabled)

### Client-Server Interaction

```mermaid
sequenceDiagram
    actor Client
    participant Server
    participant Database

    Client->>Server: Request Data
    Server->>Database: Query
    Database-->>Server: Result
    Server-->>Client: Response
    Client->>Client: Display Data
```

### Multi-party Communication

```mermaid
sequenceDiagram
    participant A as Service A
    participant B as Service B
    participant C as Service C

    A->>B: Message 1
    B->>C: Message 2
    C-->>B: Response 2
    B-->>A: Response 1
    A->>B: Confirm
```

## Class Diagram Templates (Parser-supported, lint rules not yet enabled)

### Inheritance Hierarchy

```mermaid
classDiagram
    class Animal {
        - name: string
        # age: int
        + eat()
        + sleep()
    }

    class Dog {
        + bark()
    }

    class Cat {
        + meow()
    }

    Animal <|-- Dog
    Animal <|-- Cat
```

### Composition Pattern

```mermaid
classDiagram
    class Car {
        - engine: Engine
        - wheels: Wheel[]
        + start()
        + stop()
    }

    class Engine {
        - horsepower: int
        + ignite()
    }

    class Wheel {
        - diameter: int
        + rotate()
    }

    Car *-- Engine
    Car *-- Wheel
```

## Entity-Relationship Diagram Templates (Parser-supported, lint rules not yet enabled)

### E-commerce Schema

```mermaid
erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE_ITEM : contains
    LINE_ITEM ||--|| PRODUCT : "ordered from"
    PRODUCT ||--o{ INVENTORY : "tracked by"
    CUSTOMER ||--o{ REVIEW : writes
    REVIEW ||--|| PRODUCT : "reviews"
```

## State Diagram Templates (Parser-supported, lint rules not yet enabled)

### Simple State Machine

```mermaid
stateDiagram-v2
    [*] --> Idle
    Idle --> Running: start()
    Running --> Paused: pause()
    Paused --> Running: resume()
    Running --> Idle: stop()
    Idle --> [*]
```

### Order Processing Workflow

```mermaid
stateDiagram-v2
    [*] --> Pending
    Pending --> Confirmed: confirm()
    Confirmed --> Shipped: ship()
    Shipped --> Delivered: deliver()
    Delivered --> [*]

    Pending --> Cancelled: cancel()
    Confirmed --> Cancelled: cancel()
    Cancelled --> [*]
```

## Best Practices for linting

1. **Use descriptive labels:** Clear node names make diagrams easier to understand and lint.

   ✅ Good:

   ```mermaid
   graph TD
       A[User Signup] --> B[Email Verification]
       B --> C[Account Created]
   ```

   ❌ Avoid:

   ```mermaid
   graph TD
       A[A] --> B[B]
       B --> C[C]
   ```

2. **Keep fanout reasonable:** Limit outgoing edges per node (default max is 10).

   ✅ Recommended:

   ```mermaid
   graph TD
       API[API] --> Auth[Auth Service]
       API --> User[User Service]
       API --> Order[Order Service]
   ```

   ❌ Excessive:

   ```mermaid
   graph TD
       API[API] --> A[Service 1]
       API --> B[Service 2]
       API --> C[Service 3]
       API --> D[Service 4]
       API --> E[Service 5]
   ```

3. **Avoid disconnected nodes:** Every node should have at least one connection.

   ✅ Good:

   ```mermaid
   graph TD
       A[Start] --> B[Process]
       B --> C[End]
   ```

   ❌ Avoid:

   ```mermaid
   graph TD
       A[Start] --> B[Process]
       C[Orphaned Node]
   ```

4. **No cycles for acyclic systems:** Unless explicitly intended, avoid circular dependencies.

   ✅ Good DAG:

   ```mermaid
   graph TD
       Client --> API
       API --> Cache
       Cache --> DB
   ```

   ❌ Cyclic (unless needed):

   ```mermaid
       Service A --> Service B
       Service B --> Service C
       Service C --> Service A
   ```

5. **Reasonable depth:** Web-rendered diagrams with excessive nesting may be hard to read (default max is 10 levels).

   ✅ Manageable:

   ```mermaid
   graph TD
       A --> B
       B --> C
       C --> D
       D --> E
   ```

   ❌ Deep nesting:

   ```mermaid
   graph TD
       L1 --> L2 --> L3 --> L4 --> L5 --> L6 --> L7 --> L8 --> L9 --> L10 --> L11
   ```

## Common Linting Issues and Fixes

### Issue: "Diagram type is not supported for linting"

**Cause:** You're using a diagram type that merm8 doesn't lint yet (sequence, class, state, ER).

**Fix:** Either:

- Rewrite as a flowchart (Mermaid directive `graph` or `flowchart`)
- Wait for future releases with broader linting support

### Issue: Duplicate Node IDs

**Error:** `core/no-duplicate-node-ids`

**Example:**

```mermaid
graph TD
    A[Node] --> B[Node]
    A[Same ID] --> C[End]
```

**Fix:** Rename one of the conflicting nodes.

### Issue: Disconnected Nodes

**Error:** `core/no-disconnected-nodes`

**Example:**

```mermaid
graph TD
    A --> B
    C[Orphan]
```

**Fix:** Connect orphan nodes or suppress them in config.

### Issue: High Fanout

**Error:** `core/max-fanout` (default limit: 10)

**Example:**

```mermaid
graph TD
    Hub[Central Hub]
    Hub --> 1[(DB1)]
    Hub --> 2[(DB2)]
    Hub --> 3[(DB3)]
    Hub --> N[(DB12)]
```

**Fix:** Introduce intermediate nodes or increase limit in config.

### Issue: Excessive Depth

**Error:** `core/max-depth` (default limit: 10)

**Example:**

```mermaid
graph TD
    Start --> L1 --> L2 --> L3 --> L4 --> L5 --> L6 --> L7 --> L8 --> L9 --> L10 --> L11
```

**Fix:** Refactor into parallel branches or increase limit in config.

### Issue: Circular Dependencies

**Error:** `core/no-cycles`

**Example:**

```mermaid
graph TD
    A --> B
    B --> C
    C --> A
```

**Fix:** Remove one edge to break the cycle or suppress in config if intentional.
