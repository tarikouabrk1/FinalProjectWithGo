# 🔄 Reverse Proxy HTTP en Go

Un reverse proxy HTTP performant et thread-safe implémenté en Go, avec load balancing intelligent, health checks automatiques et une API d'administration complète.

## ✨ Fonctionnalités

- **Load Balancing Multi-Stratégies**
  - Round-Robin : distribution équitable des requêtes en rotation
  - Least-Connections : routage intelligent vers le backend le moins chargé

- **Health Checks Automatiques**
  - Vérification périodique de l'état des backends via `/health`
  - Désactivation automatique des backends défaillants
  - Réactivation automatique lors de la récupération
  - Fréquence configurable

- **API d'Administration**
  - Ajout/suppression dynamique de backends
  - Consultation du statut en temps réel
  - Surveillance des connexions actives par backend

- **Robustesse**
  - Thread-safe avec mutex et atomic operations
  - Gestion des timeouts et annulations client
  - Gestion d'erreurs complète avec failover automatique

## 📋 Prérequis

- Go 1.19 ou supérieur
- Backends HTTP avec endpoint `/health` (obligatoire pour les health checks)

## 🚀 Installation et Démarrage

### 1. Cloner le projet

```bash
git clone <repository-url>
cd reverse-proxy
```

### 2. Configuration

Éditez `config/config.json` selon vos besoins :

```json
{
  "port": 8080,
  "admin_port": 8081,
  "strategy": "round-robin",
  "health_check_frequency": 1,
  "backends": [
    "http://localhost:8082",
    "http://localhost:8083"
  ]
}
```

**Paramètres :**
- `port` : Port du reverse proxy (défaut: 8080)
- `admin_port` : Port de l'API d'administration (défaut: 8081)
- `strategy` : `"round-robin"` ou `"least-connections"`
- `health_check_frequency` : Intervalle en secondes entre les health checks (défaut: 1)
- `backends` : Liste des URLs des backends à load balancer

### 3. Démarrer les backends de test

**Terminal 1 - Backend sur port 8082 :**
```bash
go run backend1/backend1.go
```

**Terminal 2 - Backend sur port 8083 :**
```bash
go run backend2/backend2.go
```

### 4. Lancer le reverse proxy

**Terminal 3 :**
```bash
go run main.go
```

**Sortie attendue :**
```
✓ Backend http://localhost:8082 is healthy
✓ Backend http://localhost:8083 is healthy
2/2 backends are healthy
Health checker started (interval: 1s)
Admin API running on :8081
Reverse Proxy running on :8080 (strategy: round-robin)
```

## 🎯 Stratégies de Load Balancing

### 1️⃣ Round-Robin

**Principe :** Distribue les requêtes de manière cyclique entre tous les backends disponibles.

**Cas d'usage :** Idéal quand les backends ont des capacités similaires et que les requêtes ont un coût de traitement équivalent.

#### Configuration

```json
{
  "strategy": "round-robin"
}
```

#### Test de la stratégie Round-Robin

**Requêtes séquentielles (PowerShell) :**
```powershell
# Envoyer 6 requêtes successives
for ($i=1; $i -le 6; $i++) {
    $response = Invoke-WebRequest -Uri http://localhost:8080 -UseBasicParsing
    Write-Host "Request $i`: $($response.Content.Trim())"
}
```

**Résultat attendu (alternance parfaite) :**
```
Request 1: Hello from backend 8083
Request 2: Hello from backend 8082
Request 3: Hello from backend 8083
Request 4: Hello from backend 8082
Request 5: Hello from backend 8083
Request 6: Hello from backend 8082
```

**Visualisation en temps réel :**
```bash
# Linux/Mac
while true; do curl http://localhost:8080; sleep 1; done

# Windows PowerShell
while($true) { 
    (Invoke-WebRequest http://localhost:8080 -UseBasicParsing).Content
    Start-Sleep -Seconds 1 
}
```

#### Comportement

- ✅ Distribution équitable : chaque backend reçoit le même nombre de requêtes
- ✅ Prévisible : ordre cyclique constant (A → B → A → B)
- ✅ Simple : pas de calculs complexes
- ⚠️ Ne tient pas compte de la charge réelle des backends

---

### 2️⃣ Least-Connections

**Principe :** Envoie chaque nouvelle requête au backend ayant le moins de connexions actives.

**Cas d'usage :** Idéal quand les requêtes ont des temps de traitement variables ou quand les backends ont des capacités différentes.

#### Configuration

```json
{
  "strategy": "least-connections"
}
```

#### Test de la stratégie Least-Connections

**⚠️ Important :** Pour observer l'effet de least-connections, il faut envoyer des **requêtes concurrentes** (simultanées), pas séquentielles.

**Test avec requêtes concurrentes (PowerShell) :**
```powershell
# Envoyer 20 requêtes simultanées
$results = 1..20 | ForEach-Object { 
    Start-Job -ScriptBlock { 
        (Invoke-WebRequest -Uri http://localhost:8080 -UseBasicParsing).Content.Trim()
    } 
} | Wait-Job | Receive-Job

$count8082 = ($results | Where-Object { $_ -like "*8082*" }).Count
$count8083 = ($results | Where-Object { $_ -like "*8083*" }).Count

Write-Host "Backend 8082: $count8082 requests" -ForegroundColor Green
Write-Host "Backend 8083: $count8083 requests" -ForegroundColor Green
```

**Résultat attendu (distribution équilibrée) :**
```
Backend 8082: 10 requests
Backend 8083: 10 requests
```
*Note : Variation de ±1 requête est normale (ex: 9-11, 11-9)*


#### Comportement

- ✅ Équilibrage dynamique : s'adapte à la charge réelle
- ✅ Optimal pour requêtes hétérogènes : gère bien les requêtes lentes vs rapides
- ✅ Prévient la surcharge : évite qu'un backend soit submergé
- ⚠️ Requêtes séquentielles iront toujours au même backend (normal, car tous à 0 connexions)

---

## 📡 API d'Administration

### Consulter le statut global

```bash
GET http://localhost:8081/status
```

**Exemple avec curl :**
```bash
curl http://localhost:8081/status | python3 -m json.tool
```

**Réponse avec backends actifs :**
```json
{
  "total_backends": 2,
  "active_backends": 2,
  "backends": [
    {
      "url": "http://localhost:8082",
      "alive": true,
      "current_connections": 0
    },
    {
      "url": "http://localhost:8083",
      "alive": true,
      "current_connections": 1
    }
  ]
}
```

**Réponse si backends arrêtés :**
```json
{
  "total_backends": 2,
  "active_backends": 0,
  "backends": [
    {
      "url": "http://localhost:8082",
      "alive": false,
      "current_connections": 0
    },
    {
      "url": "http://localhost:8083",
      "alive": false,
      "current_connections": 0
    }
  ]
}
```

### Ajouter un backend dynamiquement

```bash
POST http://localhost:8081/backends
Content-Type: application/json

{
  "url": "http://localhost:8084"
}
```

**Exemple avec curl :**
```bash
curl -X POST http://localhost:8081/backends \
  -H "Content-Type: application/json" \
  -d '{"url": "http://localhost:8084"}'
```

**Réponse :** `201 Created`

**Note :** Le backend sera automatiquement vérifié par le health checker dans les secondes suivantes.

### Supprimer un backend

```bash
DELETE http://localhost:8081/backends
Content-Type: application/json

{
  "url": "http://localhost:8084"
}
```

**Exemple avec curl :**
```bash
curl -X DELETE http://localhost:8081/backends \
  -H "Content-Type: application/json" \
  -d '{"url": "http://localhost:8084"}'
```

**Réponse :** `204 No Content`

---

## 🧪 Scénarios de Test Complets

### Test 1 : Failover automatique

**Objectif :** Vérifier que le proxy détecte les pannes et route uniquement vers les backends sains.

```bash
# 1. Vérifier que les 2 backends sont actifs
curl http://localhost:8081/status

# 2. Arrêter le backend 8082 (Ctrl+C dans son terminal)

# 3. Attendre 1-2 secondes (health check)

# 4. Vérifier le statut
curl http://localhost:8081/status
# → backend 8082 doit être "alive": false

# 5. Envoyer des requêtes
curl http://localhost:8080
curl http://localhost:8080
curl http://localhost:8080
# → Toutes les réponses doivent venir de 8083

# 6. Redémarrer le backend 8082
go run backend1/backend1.go

# 7. Attendre 10 secondes puis vérifier
curl http://localhost:8081/status
# → backend 8082 doit être "alive": true
```


### Test 3 : Gestion des backends lents

```bash
# Modifier backend1.go pour ajouter un délai de 10 secondes
# time.Sleep(10 * time.Second)

# Avec round-robin : les requêtes attendront toutes ~3 secondes en moyenne
# Avec least-connections : le backend lent recevra moins de requêtes
```

---

## 🏗️ Architecture du Projet

```
FinalProjectWithGo/
├── readme.md
├── go.mod
├── main.go
├── Final Project - Reverse Proxy.pdf
│
├── admin/
│   └── admin.go
│
├── backend1/
│   └── backend1.go
│
├── backend2/
│   └── backend2.go
│
├── config/
│   └── config.json
│
├── health/
│   ├── checker.go
│   └── checker_test.go
│
├── pool/
│   ├── server_pool.go
│   └── server_pool_test.go
│
├── proxy/
│   ├── proxy.go
│   └── proxy_test.go
```

### Flux d'une requête

```
Client → Reverse Proxy (port 8080)
         ↓
    GetNextValidPeer()
         ↓
    Round-Robin OU Least-Connections
         ↓
    Sélection d'un backend
         ↓
    Incrémentation compteur connexions
         ↓
    Proxy vers backend
         ↓
    Décrémentation compteur
         ↓
    Réponse au client
```

---

## 🔧 Détails Techniques

### Thread Safety

- **sync.RWMutex** : Protège l'accès concurrent au slice de backends
  - `RLock` pour les lectures (GetNextValidPeer)
  - `Lock` pour les modifications (AddBackend, RemoveBackend)
- **atomic.AddInt64** : Gestion thread-safe des compteurs de connexions
- **atomic.AddUint64** : Incrémentation du compteur round-robin
- Aucune race condition grâce à ces mécanismes

### Gestion des Timeouts

| Opération | Timeout | Raison |
|-----------|---------|--------|
| Requêtes proxifiées | 30s | Évite les requêtes bloquées indéfiniment |
| Health checks | 2s | Détection rapide des backends inactifs |
| Client cancellation | Propagé | Respect des annulations côté client |

### Load Balancing - Implémentation

**Round-Robin :**
```go
start := atomic.AddUint64(&s.Current, 1) % uint64(length)
// Incrémente un compteur global, modulo le nombre de backends
// Garantit une distribution cyclique équitable
```

**Least-Connections :**
```go
for _, b := range s.Backends {
    conns := atomic.LoadInt64(&b.CurrentConns)
    if b.IsAlive() && conns < minConns {
        best = b
        minConns = conns
    }
}
// Parcourt tous les backends et sélectionne celui avec le minimum de connexions
```

### Health Checks

- Vérification périodique via endpoint `/health`
- Transition automatique des états :
  - `UP → DOWN` : Si `/health` retourne erreur ou status != 200
  - `DOWN → UP` : Si `/health` retourne 200 OK
- Logs des changements d'état pour debugging

---

## 📊 Comparaison des Stratégies

| Critère | Round-Robin | Least-Connections |
|---------|-------------|-------------------|
| **Simplicité** |  Très simple | Moyennement simple |
| **Performance** | Bonne | Excellente |
| **Équilibrage** | ✅ Équitable sur le long terme | ✅ Optimal en temps réel |
| **Backends hétérogènes** | Moins adapté | Très adapté |
| **Requêtes variables** | Peut créer des déséquilibres | S'adapte automatiquement |
| **CPU utilisé** |  Minimal | Légèrement supérieur |
| **Cas d'usage** | Backends identiques, requêtes similaires | Backends différents, requêtes hétérogènes |

---

## 👨‍💻 Auteur

Développé par Tarik Ouabrk en Go
