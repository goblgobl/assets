# Asset Server
Early in development.

This server is meant to sit infront of an origin server (like S3) and serve static assets (e.g. js, css, images, ...)

Aside from aggersive caching and general focus towards performance, the main value of this project is to provide basic image transformation. [vipsthumbnail](https://www.libvips.org/API/current/Using-vipsthumbnail.html) is used for image transformation (and must be installed).

