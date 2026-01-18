# 🔄 Reverse Proxy HTTP en Go

Un reverse proxy HTTP performant et thread-safe implémenté en Go, avec load balancing intelligent, health checks automatiques et une API d'administration complète.

## ✨ Fonctionnalités

- **Load Balancing Multi-Stratégies**
  - Round-Robin : distribution équitable des requêtes
  - Least-Connections : routage vers le backend le moins chargé

- **Health Checks Automatiques**
  - Vérification périodique de l'état des backends via `/health`
  - Désactivation automatique des backends défaillants
  - Fréquence configurable

- **API d'Administration**
  - Ajout/suppression dynamique de backends
  - Consultation du statut en temps réel
  - Surveillance des connexions actives

- **Robustesse**
  - Thread-safe avec mutex et atomic operations
  - Gestion des timeouts et annulations client
  - Gestion d'erreurs complète
