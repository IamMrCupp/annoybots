# Changelog

## [1.2.0](https://github.com/IamMrCupp/annoybots/compare/v1.1.0...v1.2.0) (2026-06-14)


### Features

* **engine:** timer-driven ambient chatter (Track A) ([cc591b2](https://github.com/IamMrCupp/annoybots/commit/cc591b217a4abf3ed98670aa90f3a05deffe5580))
* **engine:** timer-driven ambient chatter into quiet-but-live channels ([5a5364d](https://github.com/IamMrCupp/annoybots/commit/5a5364dcea145892daafef3df2dfa6e6406aeaf4))

## [1.1.0](https://github.com/IamMrCupp/annoybots/compare/v1.0.3...v1.1.0) (2026-06-14)


### Features

* **irc:** NickServ IDENTIFY on connect for networks without SASL ([136d4d4](https://github.com/IamMrCupp/annoybots/commit/136d4d441cbb78fef2c9e9e22dd5ff8e3fbe8918))
* **irc:** NickServ IDENTIFY on connect for non-SASL networks ([284998e](https://github.com/IamMrCupp/annoybots/commit/284998e813f7d14573f580387b1c1012969b4b96))

## [1.0.3](https://github.com/IamMrCupp/annoybots/compare/v1.0.2...v1.0.3) (2026-06-14)


### Refactoring

* **configs:** replace named bots with neutral Echo/Mimic examples ([3e04b67](https://github.com/IamMrCupp/annoybots/commit/3e04b670afdfd3aaa8b4b8d479a8e7387a9bd4e9))
* **configs:** replace named bots with neutral Echo/Mimic examples ([95dd87a](https://github.com/IamMrCupp/annoybots/commit/95dd87a54447be32d2fa3505baac5dac617f39b8))
* **deploy:** genericize overlays to echo/mimic; make SOPS optional ([c464295](https://github.com/IamMrCupp/annoybots/commit/c46429532e37143d850e33012fd05c70e83dacfa))
* **deploy:** genericize overlays to echo/mimic; make SOPS optional ([965441d](https://github.com/IamMrCupp/annoybots/commit/965441d887a0c76f44071ce938e3447742a43763))


### Documentation

* add CONTRIBUTING, SECURITY, and issue/PR templates ([ffa3677](https://github.com/IamMrCupp/annoybots/commit/ffa36778b840585637446f058d90daa46af48685))
* add CONTRIBUTING, SECURITY, and issue/PR templates ([dd89524](https://github.com/IamMrCupp/annoybots/commit/dd895242818b175dd468c40b64e589fec3f8997d))
* **readme:** reframe as reusable framework, add "Add a bot" guide ([de22024](https://github.com/IamMrCupp/annoybots/commit/de2202483403b7b30f85aaf8226c2cfc76c9bf0f))
* **readme:** reframe as reusable framework, add "Add a bot" guide ([b0f32fa](https://github.com/IamMrCupp/annoybots/commit/b0f32fa20b2ecd33192a022eaebdff3e5467efca))

## [1.0.2](https://github.com/IamMrCupp/annoybots/compare/v1.0.1...v1.0.2) (2026-06-13)


### CI/CD

* build multi-arch (amd64+arm64) release image ([2e7df6a](https://github.com/IamMrCupp/annoybots/commit/2e7df6a5f0301de7e3d94cfb3608f7b9fef6e041))
* build multi-arch (amd64+arm64) release image ([6c34162](https://github.com/IamMrCupp/annoybots/commit/6c341629f6ea968c4ff3506b6c96aec669a0d911))

## [1.0.1](https://github.com/IamMrCupp/annoybots/compare/v1.0.0...v1.0.1) (2026-06-12)


### Documentation

* add MIT LICENSE ([84b10b5](https://github.com/IamMrCupp/annoybots/commit/84b10b5890c05d11494aa9be310d7f9c06b7945f))
* add MIT LICENSE ([c72fcdf](https://github.com/IamMrCupp/annoybots/commit/c72fcdf79c65a4423850791e048166843b8edfc3))

## 1.0.0 (2026-06-12)


### Features

* add !reload admin command for quotes and skits ([3bf78ed](https://github.com/IamMrCupp/annoybots/commit/3bf78edf161cb3dd23f3abcd3f7329050cdf4588))
* add /source slash command and Discord mention handling ([22baef4](https://github.com/IamMrCupp/annoybots/commit/22baef4151b52377eca48db5258bda3f0671786f))
* add chat-based admin console (identity auth, DM-only) ([f5f4188](https://github.com/IamMrCupp/annoybots/commit/f5f418819f8b68de6ed6ae8bef1a09bd01efaa83))
* add Discord transport with slash commands ([fc84abd](https://github.com/IamMrCupp/annoybots/commit/fc84abddc68d5f341375b5be92f96a57b7bffe10))
* add Futurama quote pack ([5dad4b4](https://github.com/IamMrCupp/annoybots/commit/5dad4b42324766d0cc50d8ef4a7343c09aeb47ed))
* add inter-bot botnet (sibling banter + coordinated skits) ([81847da](https://github.com/IamMrCupp/annoybots/commit/81847dad06ccb7a99c95ebe972992e340453b774))
* add password (!login) auth fallback to admin console ([8953280](https://github.com/IamMrCupp/annoybots/commit/8953280f81477d3faa643fed10ce09eba87a9116))
* add Snuff Box quote pack ([68dfe57](https://github.com/IamMrCupp/annoybots/commit/68dfe577f6466c91081bbed1fd8492004955f15b))
* add Snuff Box skit and !packs/\/packs discovery command ([fb08bbc](https://github.com/IamMrCupp/annoybots/commit/fb08bbc30ef723e78d03d8d1bb20c17228d4b97f))
* add Tim and Eric, Squidbillies, and Orgazmo quote packs ([832abee](https://github.com/IamMrCupp/annoybots/commit/832abee8557e40cc794f0d72c5403f62b061b96f))
* GitOps deploy (Flux, SOPS, ConfigMap content, release-please) ([7ebe6e2](https://github.com/IamMrCupp/annoybots/commit/7ebe6e2e290960445a65abc196872cce7a58461f))
* rebirth Arywen and Kurkutu as containerized IRC/Twitch bots ([3b68efa](https://github.com/IamMrCupp/annoybots/commit/3b68efa400af35ff90b4b4fe5bc9daba975ab2aa))
* uncensor quote packs for adult channels ([12accc2](https://github.com/IamMrCupp/annoybots/commit/12accc2939bbb5ac5ebc398f1a3e4bf4a0248e05))


### Documentation

* add Flux aggregator kustomization and starter CHANGELOG ([c2a08fc](https://github.com/IamMrCupp/annoybots/commit/c2a08fc044b1c0d82ce238cc62edad6f0447a502))

## Changelog

This file is maintained by [release-please](https://github.com/googleapis/release-please)
from Conventional Commit messages. Release sections below are generated
automatically when a release PR is merged.
