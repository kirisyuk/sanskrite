export interface ExternalDocRef {
  provider: string; // e.g., gdrive | onedrive | box | sharepoint
  url: string;      // canonical view URL
  resourceId?: string;
  driveId?: string;
  ownerEmail?: string;
  eTag?: string;
  mimeType?: string;
}

export interface CoreDoc {
  id: string;
  tenantId: string;
  projectId: string;
  filename: string;
  contentType: string;
  description?: string;
  sizeBytes?: number;
  storagePath?: string;       // gs://bucket/path or similar
  external?: ExternalDocRef;  // if referenced externally
  createdBy?: string;
  createdAt: string;          // ISO string in frontend
  updatedAt: string;          // ISO string in frontend
}


