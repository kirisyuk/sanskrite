/// <reference types="vite/client" />

declare module '*.png' {
  const src: string;
  export default src;
}

declare module 'topojson-client' {
  export function feature(topology: object, object: object): GeoJSON.FeatureCollection;
}

