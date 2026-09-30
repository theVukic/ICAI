import "./style.css";
import "notyf/notyf.min.css";
import { mountBulkCategoryPage, setBulkCategoryPageSession } from "./bulk-category";
import { mountCategoryPage, prefetchCategoryPage, setCategoryPageSession } from "./category";
import { mountBulkEditPage, setBulkEditPageSession } from "./bulk-edit";
import { mountDescriptionPage, setDescriptionPageSession } from "./description";
import { mountEditPage, setEditPageSession } from "./edit";
import { renderAuthLoading, renderLoginScreen } from "./login";
import { Notyf } from "notyf";
import { getPublishSessionStats, recordPublishSessionOutcome } from "./publish-session";
import {
  fetchListingLimits,
  loadSessionInfo,
  loginApp,
  logoutApp,
  switchOlxToken,
  resetSessionInfoCache,
  type ListingLimits,
} from "./session";
import { mountPageTransition, navigateWithFade } from "./transition";
import { renderAppBrand } from "./branding";
import { installGlobalClientLogging, logClientEvent } from "./logger";
import {
  openImageManager,
  pickAndUploadBulkImages,
} from "./image-manager";

type CatalogImage = {
  id: string;
  position: number;
  url: string;
  source?: string;
};

type CatalogStatus = {
  code: string;
  name: string;
};

type CatalogItem = {
	itemId: string;
	selectionKey?: string;
  towKod: string;
  index: string;
  nameB2B: string;
  brandName: string;
  images: CatalogImage[];
  processedImageUrl?: string;
  technicalDescription: string;
  originalTechnicalDescription?: string;
	aiTechnicalDescription?: string;
	useAIContent?: boolean;
  technicalDescriptionFormat?: "plain" | "html";
  status: CatalogStatus;
  retailPrice: number;
  currencyCode: string;
  sourceFileName?: string;
  categorization?: {
    category?: {
      id: number;
      name: string;
    };
  };
};

function getItemSelectionKey(item: CatalogItem): string {
	return item.itemId;
}

type UploadedCatalog = {
  id: string;
  fileName: string;
  savedPath: string;
  uploadedAt: string;
  totalItems: number;
};

type CatalogState = {
  catalogs: UploadedCatalog[];
  items: CatalogItem[];
  tempDir: string;
  totalFiles: number;
  totalItems: number;
  lastUpdated: string | null;
  imageProcessing: ImageProcessingState;
  aiProcessing: AIProcessingState;
};

type ImageProcessingItem = { status: "queued" | "processing" | "done" | "error"; error?: string; model?: "fast" | "detailed" };
type ImageProcessingState = {
  enabled: boolean;
  aiReady: boolean;
  aiError?: string;
  items: Record<string, ImageProcessingItem>;
  queued: number;
  working: number;
  done: number;
  failed: number;
};

type DescriptionProcessingItem = { status: "" | "queued" | "processing" | "done" | "error"; error?: string; source?: "web" | "catalog" | "cache" | "manual"; model?: string };
type AIProcessingItem = { image: ImageProcessingItem; description: DescriptionProcessingItem };
type AIProcessingState = {
  enabled: boolean;
  aiReady: boolean;
  aiError?: string;
  items: Record<string, AIProcessingItem>;
  queued: number;
  working: number;
  done: number;
  failed: number;
};

type ErrorResponse = {
  error: string;
};

type UpdateStatus = {
  checked: boolean;
  available: boolean;
  updated?: boolean;
  version?: string;
  currentVersion: string;
  notes?: string;
  error?: string;
  nonce: string;
};

type CatalogResponse = CatalogState | ErrorResponse;

type PublishResponse = {
  message: string;
  published: Array<{
    towKod: string;
    title: string;
    listingId: number;
    published: boolean;
    publishedAt?: string;
    message?: string;
  }>;
  skipped: Array<{
    towKod: string;
    title: string;
    reason: string;
  }>;
  failed: Array<{
    towKod: string;
    title: string;
    error: string;
  }>;
  state: CatalogState;
  listingLimits?: ListingLimits;
};

type PublishOptimisticSnapshot = {
  selectedTowKods: string[];
  selectAllMode: boolean;
  selectionPulseTowKods: string[];
};

const VIEW_MODE_STORAGE_KEY = "asiapp:view-mode";
const SORT_MODE_STORAGE_KEY = "asiapp:sort-mode";
const SELECTION_MODE_STORAGE_KEY = "asiapp:selection-mode";
const SELECT_ALL_STORAGE_KEY = "asiapp:select-all";
const SELECTED_TOWKODS_STORAGE_KEY = "asiapp:selected-towkods";
const MAIN_SCROLL_TOP_STORAGE_KEY = "asiapp:main-scroll-top";
const AUTH_POLL_INTERVAL_MS = 600000;

type SortMode = "price-asc" | "price-desc" | "name" | "brand" | "publish-ready";

const SORT_MODE_LABELS: Record<SortMode, string> = {
  "price-asc": "Cijena najniza",
  "price-desc": "Cijena najvisa",
  name: "Ime",
  brand: "Brend",
  "publish-ready": "Po objavi",
};

const app = document.querySelector<HTMLDivElement>("#app");

if (!app) {
  throw new Error("No #app element found.");
}

const root = app;
installGlobalClientLogging();
mountPageTransition(root);
let authPollTimer: number | null = null;
let loadStateInFlight: Promise<void> | null = null;
const descriptionPreviewScrollPositions = new Map<string, number>();
let updatePromptShown = false;
let versionToastShown = false;

const notyf = new Notyf({
  duration: 0,
  position: { x: "right", y: "top" },
  dismissible: true,
  ripple: false,
  types: [
    {
      type: "warning",
      background: "linear-gradient(135deg, #d6a62f, #b88310)",
      icon: false,
      className: "notyf__toast--warning",
    },
    {
      type: "update",
      background: "linear-gradient(135deg, #1976d2, #0d47a1)",
      icon: false,
      className: "notyf__toast--update",
      dismissible: false,
    },
    {
      type: "version",
      background: "linear-gradient(135deg, #1976d2, #0d47a1)",
      icon: false,
      className: "notyf__toast--version",
      dismissible: true,
    },
  ],
});

const state: {
  data: CatalogState;
  isDragging: boolean;
  isUploading: boolean;
  isPickingFiles: boolean;
  message: string;
  messageTone: "muted" | "success" | "danger";
  imageIndexes: Record<string, number>;
  viewMode: "grid" | "list";
  sortMode: SortMode;
  sortMenuOpen: boolean;
  selectionMode: boolean;
  selectionAnimation: "" | "animate-on" | "animate-off";
  selectionToolsVisible: boolean;
  selectionToolsAnimation: "" | "selection-tools-enter" | "selection-tools-exit";
  selectAllMode: boolean;
  selectedTowKods: string[];
  isDeletingSelected: boolean;
  activePublishCount: number;
  selectionPulseTowKods: string[];
  animateItemsIn: boolean;
  sessionUsername: string;
  tokenConfigured: boolean;
  isAuthenticated: boolean;
  authChecking: boolean;
  authSubmitting: boolean;
  authError: string;
  showFieldError: boolean;
  appUsername: string;
  appRole: string;
  rememberMe: boolean;
  olxProfileName: string;
  olxAvailableTokens: string[];
  olxTokenMenuOpen: boolean;
  olxTokenSwitching: boolean;
  listingLimitRemaining: number | null;
  listingLimitUnlimited: boolean;
  listingLimitLoading: boolean;
  shouldRestoreMainScroll: boolean;
  refreshSuspendUntil: number;
  bulkOptionsOpen: boolean;
} = {
  data: {
    catalogs: [],
    items: [],
    tempDir: "",
    totalFiles: 0,
    totalItems: 0,
    lastUpdated: null,
    imageProcessing: { enabled: false, aiReady: false, items: {}, queued: 0, working: 0, done: 0, failed: 0 },
    aiProcessing: { enabled: false, aiReady: false, items: {}, queued: 0, working: 0, done: 0, failed: 0 },
  },
  isDragging: false,
  isUploading: false,
  isPickingFiles: false,
  message: "Prevuci JSON fajl ili klikni na Dodaj jos.",
  messageTone: "muted",
  imageIndexes: {},
  viewMode: loadStoredViewMode(),
  sortMode: loadStoredSortMode(),
  sortMenuOpen: false,
  selectionMode: loadSessionSelectionMode(),
  selectionAnimation: "",
  selectionToolsVisible: loadSessionSelectionMode(),
  selectionToolsAnimation: "",
  selectAllMode: loadStoredSelectAllMode(),
  selectedTowKods: loadStoredSelectedTowKods(),
  isDeletingSelected: false,
  activePublishCount: 0,
  selectionPulseTowKods: [],
  animateItemsIn: false,
  sessionUsername: "",
  tokenConfigured: false,
  isAuthenticated: false,
  authChecking: true,
  authSubmitting: false,
  authError: "",
  showFieldError: false,
  appUsername: "",
  appRole: "",
  rememberMe: false,
  olxProfileName: "",
  olxAvailableTokens: [],
  olxTokenMenuOpen: false,
  olxTokenSwitching: false,
  listingLimitRemaining: null,
  listingLimitUnlimited: false,
  listingLimitLoading: false,
  shouldRestoreMainScroll: false,
  refreshSuspendUntil: 0,
  bulkOptionsOpen: false,
};

const plusIcon = `
  <svg viewBox="0 0 800 800" aria-hidden="true">
    <path d="m400 179.58c-121.23 0-220.42 99.188-220.42 220.42S278.768 620.42 400 620.42 620.42 521.232 620.42 400 521.232 179.58 400 179.58zm110.21 236.16h-94.465v94.465c0 8.6602-7.0859 15.742-15.742 15.742-8.6602 0-15.742-7.0859-15.742-15.742V415.74h-94.465c-8.6602 0-15.742-7.0859-15.742-15.742 0-8.6602 7.0859-15.742 15.742-15.742h94.465v-94.465c0-8.6602 7.0859-15.742 15.742-15.742 8.6602 0 15.742 7.0859 15.742 15.742v94.465h94.465c8.6602 0 15.742 7.0859 15.742 15.742 0 8.6602-7.0859 15.742-15.742 15.742z" />
  </svg>
`;

const centerPlusIcon = `
  <svg viewBox="0 0 800 800" aria-hidden="true">
    <path d="m398.89 203.36c-5.1875.57031-9.0859 5.0117-8.9727 10.234v176.33h-176.33c-.31641-.015625-.62891-.015625-.94531 0-2.6719.125-5.1875 1.3086-6.9883 3.2852-1.8008 1.9766-2.7422 4.5898-2.6172 7.2656.125 2.6719 1.3086 5.1836 3.2852 6.9844 1.9805 1.8008 4.5938 2.7422 7.2656 2.6172h176.33v176.33c-.039063 2.6992 1.0078 5.2969 2.9023 7.2188 1.8906 1.9219 4.4766 3.0039 7.1758 3.0039 2.6953 0 5.2812-1.082 7.1758-3.0039 1.8906-1.9219 2.9375-4.5195 2.8984-7.2188v-176.33h176.33c2.6992.039062 5.2969-1.0078 7.2188-2.8984 1.9219-1.8945 3.0039-4.4805 3.0039-7.1758 0-2.6992-1.082-5.2852-3.0039-7.1758-1.9219-1.8945-4.5195-2.9414-7.2188-2.9023h-176.33v-176.33c.0625-2.9023-1.1289-5.6914-3.2695-7.6523-2.1406-1.957-5.0234-2.8984-7.9102-2.582z" />
  </svg>
`;

const deleteIcon = `
  <svg viewBox="0 0 800 800" aria-hidden="true">
    <path d="m366.45 387.96h9.3203v92.449H366.45z" />
    <path d="m424.28 387.96h9.3203v92.449H424.28z" />
    <path d="m395.36 387.96h9.3203v92.449H395.36z" />
    <path d="m545.3 208.55H254.7c-12.238 0-23.977 4.8633-32.633 13.516-8.6523 8.6562-13.516 20.395-13.516 32.633v290.6c0 12.238 4.8633 23.977 13.516 32.633 8.6562 8.6562 20.395 13.516 32.633 13.516h290.6c12.238 0 23.977-4.8594 32.633-13.516s13.516-20.395 13.516-32.633v-290.6c0-12.238-4.8594-23.977-13.516-32.633-8.6562-8.6523-20.395-13.516-32.633-13.516zm-176.33 97.539c0-7.5703 6.1328-13.703 13.703-13.703h34.562c3.6328 0 7.1172 1.4414 9.6875 4.0117 2.5703 2.5703 4.0156 6.0547 4.0156 9.6914v13.301h-61.918zm84.641 183.44c-.51562 4.9766-2.8633 9.5859-6.5859 12.93-3.7266 3.3438-8.5625 5.1797-13.566 5.1562h-66.906c-5.0078.023438-9.8438-1.8125-13.566-5.1562-3.7266-3.3438-6.0742-7.9531-6.5859-12.93l-13.957-121.72h135.12zm30.23-133.71c-1.7109 1.7461-4.0547 2.7305-6.5 2.7227h-154.92c-2.4375 0-4.7773-.97266-6.5-2.7031s-2.6797-4.0781-2.668-6.5156c-.027344-5.5156 2.1523-10.809 6.0508-14.707 3.8984-3.8984 9.1914-6.0781 14.707-6.0508h132.05c5.4961.011718 10.766 2.207 14.645 6.0977 3.8828 3.8906 6.0625 9.1641 6.0625 14.66.015625 2.4336-.94531 4.7734-2.668 6.4961z" />
  </svg>
`;

const saveIcon = `
  <svg viewBox="144 144 512 512" aria-hidden="true">
    <path d="m276.2 601.54h-13.375c-11.234 0-16.852-13.602-8.8672-21.512l128.04-127.36c9.625-9.5977 9.6758-25.168 0.10156-34.812-9.6211-9.7266-25.34-9.7266-34.965 0l-127.14 128.14c-7.9102 7.9883-21.535 2.3711-21.535-8.8672v-11.156c0-13.906-11.262-25.191-25.191-25.191-13.906 0-25.191 11.285-25.191 25.191v75.57c0 27.809 22.57 50.379 50.383 50.379h77.734c13.781 0 24.992-11.059 25.191-24.812 0.20312-14.055-11.133-25.566-25.191-25.566m375.69-289.92v289.92c0 27.809-22.543 50.379-50.379 50.379h-226.71c-13.906 0-25.191-11.285-25.191-25.188 0-13.906 11.285-25.191 25.191-25.191h201.52c13.93 0 25.191-11.285 25.191-25.191v-226.71h-100.76c-27.812 0-50.383-22.57-50.383-50.379v-100.76h-226.71c-13.902 0-25.188 11.285-25.188 25.191v201.52c0 13.902-11.262 25.188-25.191 25.188-13.906 0-25.191-11.285-25.191-25.188v-226.71c0-27.836 22.57-50.383 50.383-50.383h291.75c6.75 0 13.199 2.6953 17.938 7.5078l136.48 138.32c4.6602 4.7109 7.2539 11.059 7.2539 17.684" fill-rule="evenodd" />
  </svg>
`;

const publishIcon = `
  <svg viewBox="0 0 800 800" aria-hidden="true">
    <path d="m548.62 265.07c-8.5703-13.957-23.766-22.477-40.145-22.512h-216.95c-16.383.035156-31.574 8.5547-40.148 22.512l-64.551 103.44c-4.7422 7.543-7.2539 16.277-7.2422 25.188v116.51c0 12.527 4.9766 24.539 13.832 33.398 8.8594 8.8555 20.875 13.832 33.398 13.832h346.37c12.527 0 24.539-4.9766 33.398-13.832 8.8555-8.8594 13.832-20.871 13.832-33.398v-116.51c.011719-8.9102-2.5-17.645-7.2422-25.188zm-46.285 103.44c-10.441 0-20.453 4.1445-27.832 11.527-7.3828 7.3828-11.527 17.391-11.527 27.832 0 2.0859-.83203 4.0898-2.3086 5.5664-1.4766 1.4766-3.4766 2.3047-5.5664 2.3047h-110.21c-4.3477 0-7.8711-3.5234-7.8711-7.8711 0-10.441-4.1484-20.449-11.531-27.832-7.3789-7.3828-17.391-11.527-27.828-11.527h-73.84l54.316-86.75c2.8047-4.7344 7.8789-7.6602 13.383-7.7148h216.95c5.4375-.011719 10.496 2.7852 13.383 7.3984l54.316 87.066z" />
    <path d="m388.82 294.67-47.23 47.23 22.355 22.355 20.309-20.465v56.207h31.488V343.79l20.309 20.469 22.355-22.355-47.23-47.23c-2.957-2.9805-6.9805-4.6562-11.176-4.6562-4.1992 0-8.2227 1.6758-11.18 4.6562z" />
  </svg>
`;

const listViewIcon = `
  <svg viewBox="144 144 512 512" aria-hidden="true">
    <path d="M576.84 224.17H358.19c-22.168 0-40.305 18.137-40.305 40.305 0 22.168 18.137 40.305 40.305 40.305h218.65c22.168 0 40.305-18.137 40.305-40.305s-18.137-40.305-40.305-40.305z"/>
    <path d="M576.84 360.2H358.19c-22.168 0-40.305 18.137-40.305 40.305s18.137 40.305 40.305 40.305h218.65c22.168 0 40.305-18.137 40.305-40.305S599.008 360.2 576.84 360.2z"/>
    <path d="M263.97 400.5c0 22.258-18.043 40.305-40.305 40.305-22.258 0-40.305-18.047-40.305-40.305 0-22.262 18.047-40.305 40.305-40.305 22.262 0 40.305 18.043 40.305 40.305"/>
    <path d="M263.97 264.47c0 22.262-18.043 40.305-40.305 40.305-22.258 0-40.305-18.043-40.305-40.305 0-22.258 18.047-40.305 40.305-40.305 22.262 0 40.305 18.047 40.305 40.305"/>
    <path d="M576.84 496.23H358.19c-22.168 0-40.305 18.137-40.305 40.305s18.137 40.305 40.305 40.305h218.65c22.168 0 40.305-18.137 40.305-40.305s-18.137-40.305-40.305-40.305z"/>
    <path d="M263.97 536.53c0 22.258-18.043 40.305-40.305 40.305-22.258 0-40.305-18.047-40.305-40.305 0-22.262 18.047-40.305 40.305-40.305 22.262 0 40.305 18.043 40.305 40.305"/>
  </svg>
`;

const gridViewIcon = `
  <svg viewBox="0 0 52 52" aria-hidden="true">
    <path d="M14.5 4h-9C4.7 4 4 4.7 4 5.5v17C4 23.3 4.7 24 5.5 24h9c.8 0 1.5-.7 1.5-1.5v-17C16 4.7 15.3 4 14.5 4z"/>
    <path d="M30.5 4h-9C20.7 4 20 4.7 20 5.5v17c0 .8.7 1.5 1.5 1.5h9c.8 0 1.5-.7 1.5-1.5v-17C32 4.7 31.3 4 30.5 4z"/>
    <path d="M46.5 4h-9C36.7 4 36 4.7 36 5.5v17c0 .8.7 1.5 1.5 1.5h9c.8 0 1.5-.7 1.5-1.5v-17C48 4.7 47.3 4 46.5 4z"/>
    <path d="M14.5 28h-9C4.7 28 4 28.7 4 29.5v17C4 47.3 4.7 48 5.5 48h9c.8 0 1.5-.7 1.5-1.5v-17C16 28.7 15.3 28 14.5 28z"/>
    <path d="M30.5 28h-9c-.8 0-1.5.7-1.5 1.5v17c0 .8.7 1.5 1.5 1.5h9c.8 0 1.5-.7 1.5-1.5v-17C32 28.7 31.3 28 30.5 28z"/>
    <path d="M46.5 28h-9c-.8 0-1.5.7-1.5 1.5v17c0 .8.7 1.5 1.5 1.5h9c.8 0 1.5-.7 1.5-1.5v-17C48 28.7 47.3 28 46.5 28z"/>
  </svg>
`;

const sortIcon = `
  <svg viewBox="0 0 200 200" aria-hidden="true">
    <path d="M110.22 117.75h-80a10 10 0 000 20h80a10 10 0 000-20z" />
    <path d="M177.22 125.75a9.67 9.67 0 00-14 0l-8 7.5V42.75a10 10 0 00-20 0v113.5a8.29 8.29 0 003 8 9.67 9.67 0 0014 0l24.5-24.5a10.13 10.13 0 00.5-14z" />
    <path d="M110.22 37.75h-80a10 10 0 000 20h80a10 10 0 000-20z" />
    <path d="M30.22 97.75h70a10 10 0 000-20h-70a10 10 0 000 20z" />
  </svg>
`;

const selectionOffIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20.4961766 5.62668182C21.3720675 5.93447702 22 6.76890777 22 7.75v10C22 20.0972102 20.0972102 22 17.75 22h-10c-.98109223 0-1.81552298-.6279325-2.12331818-1.5038234L7.72396188 20.4995565 17.75 20.5c1.5187831 0 2.75-1.2312169 2.75-2.75v-10L20.4960194 7.69901943 20.4961766 5.62668182zM17.246813 2c1.2426407 0 2.25 1.00735931 2.25 2.25V17.246813c0 1.2426407-1.0073593 2.25-2.25 2.25H4.25c-1.24264069 0-2.25-1.0073593-2.25-2.25V4.25C2 3.00735931 3.00735931 2 4.25 2H17.246813zm0 1.5H4.25c-.41421356 0-.75.33578644-.75.75V17.246813c0 .4142136.33578644.75.75.75H17.246813c.4142136 0 .75-.3357864.75-.75V4.25c0-.41421356-.3357864-.75-.75-.75z" />
  </svg>
`;

const selectionOnIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20.4961766 5.62668182C21.3720675 5.93447702 22 6.76890777 22 7.75v10C22 20.0972102 20.0972102 22 17.75 22h-10c-.98109223 0-1.81552298-.6279325-2.12331818-1.5038234L7.72396188 20.4995565 17.75 20.5c1.5187831 0 2.75-1.2312169 2.75-2.75v-10L20.4960194 7.69901943 20.4961766 5.62668182zM17.246813 2c1.2426407 0 2.25 1.00735931 2.25 2.25V17.246813c0 1.2426407-1.0073593 2.25-2.25 2.25H4.25c-1.24264069 0-2.25-1.0073593-2.25-2.25V4.25C2 3.00735931 3.00735931 2 4.25 2H17.246813z" />
  </svg>
`;

const selectAllOffIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20.4961766 5.62668182C21.3720675 5.93447702 22 6.76890777 22 7.75v10C22 20.0972102 20.0972102 22 17.75 22h-10c-.98109223 0-1.81552298-.6279325-2.12331818-1.5038234L7.72396188 20.4995565 17.75 20.5c1.5187831 0 2.75-1.2312169 2.75-2.75v-10L20.4960194 7.69901943 20.4961766 5.62668182zM17.246813 2c1.2426407 0 2.25 1.00735931 2.25 2.25V17.246813c0 1.2426407-1.0073593 2.25-2.25 2.25H4.25c-1.24264069 0-2.25-1.0073593-2.25-2.25V4.25C2 3.00735931 3.00735931 2 4.25 2H17.246813zm0 1.5H4.25c-.41421356 0-.75.33578644-.75.75V17.246813c0 .4142136.33578644.75.75.75H17.246813c.4142136 0 .75-.3357864.75-.75V4.25c0-.41421356-.3357864-.75-.75-.75zM9.58114564 11.3581942 13.4696699 7.46966991C13.7625631 7.1767767 14.2374369 7.1767767 14.5303301 7.46966991 14.7965966 7.73593648 14.8208027 8.15260016 14.6029482 8.44621165L14.5303301 8.53033009l-4.5 4.50000001C9.73449239 13.3261678 9.26134027 13.3189471 8.97360394 13.0344464L8.9 12.95l-1.5-2C7.15147186 10.6186292 7.21862915 10.1485281 7.55 9.9 7.85124623 9.67406533 8.26714548 9.7090277 8.52699676 9.96621555L8.6 10.05 9.58114564 11.3581942 13.4696699 7.46966991 9.58114564 11.3581942z" />
  </svg>
`;

const selectAllOnIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20.4961766 5.62668182C21.3720675 5.93447702 22 6.76890777 22 7.75v10C22 20.0972102 20.0972102 22 17.75 22h-10c-.98109223 0-1.81552298-.6279325-2.12331818-1.5038234L7.72396188 20.4995565 17.75 20.5c1.5187831 0 2.75-1.2312169 2.75-2.75v-10L20.4960194 7.69901943 20.4961766 5.62668182zM17.246813 2c1.2426407 0 2.25 1.00735931 2.25 2.25V17.246813c0 1.2426407-1.0073593 2.25-2.25 2.25H4.25c-1.24264069 0-2.25-1.0073593-2.25-2.25V4.25C2 3.00735931 3.00735931 2 4.25 2H17.246813zM13.4696699 7.46966991 9.58114564 11.3581942 8.6 10.05C8.35147186 9.71862915 7.88137085 9.65147186 7.55 9.9 7.21862915 10.1485281 7.15147186 10.6186292 7.4 10.95l1.5 2C9.17384721 13.3151296 9.70759806 13.3530621 10.0303301 13.0303301l4.5-4.50000001c.2928932-.29289322.2928932-.767766959999999 0-1.06066018C14.2374369 7.1767767 13.7625631 7.1767767 13.4696699 7.46966991z" />
  </svg>
`;

const editIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M20 14.66V20a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5.34" />
    <path d="M18 2 22 6 12 16 8 16 8 12 18 2" />
  </svg>
`;

const categorizeIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path d="M17 10h2c2 0 3-1 3-3V5c0-2-1-3-3-3H17c-2 0-3 1-3 3V7c0 2 1 3 3 3z" />
    <path d="M5 22H7c2 0 3-1 3-3V17c0-2-1-3-3-3H5c-2 0-3 1-3 3v2c0 2 1 3 3 3z" />
    <path d="M6 10c2.209 0 4-1.791 4-4S8.209 2 6 2 2 3.791 2 6s1.791 4 4 4z" opacity="0.34" />
    <path d="M18 22c2.209 0 4-1.791 4-4s-1.791-4-4-4-4 1.791-4 4 1.791 4 4 4z" opacity="0.34" />
  </svg>
`;
const aiImageIcon = `<svg fill="#F3F6FB" viewBox="144 144 512 512" aria-hidden="true"><path d="M226.77 179.58c-25.895 0-47.172 21.398-47.172 47.293v346.34c0 25.895 21.277 47.203 47.172 47.203h346.34c25.895 0 47.293-21.309 47.293-47.203l-.003906-346.34c0-25.895-21.398-47.293-47.293-47.293zm0 31.488h346.34c8.9961 0 15.805 6.8125 15.805 15.805l-.003906 218.3-68.695-57.195c-8-5.793-13.934-5.4961-20.57.33594l-53.074 47.172-107.07-94.465c-6.8086-6.0781-14.117-5.4609-20.449-.37109l-107.96 88.93v-202.7c0-8.9961 6.6875-15.805 15.684-15.805zm275.55 31.488c-30.246 0-55.074 24.859-55.074 55.105s24.828 55.074 55.074 55.074 55.105-24.828 55.105-55.074c0-30.246-24.859-55.105-55.105-55.105zm0 31.52c13.23 0 23.617 10.355 23.617 23.586 0 13.23-10.387 23.586-23.617 23.586-13.23 0-23.707-10.355-23.707-23.586 0-13.23 10.48-23.586 23.707-23.586zM328.8 373.428l244.12 215.46H226.77c-8.9961 0-15.684-6.6875-15.684-15.684v-102.89zm181.67 47.418 78.445 65.344v74.906l-118.54-104.61z"/></svg>`;
const aiDescriptionIcon = `<svg fill="#F3F6FB" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 7H3V4A1 1 0 0 1 4 3H20a1 1 0 0 1 1 1zM3 20V9H21V20a1 1 0 0 1-1 1H4A1 1 0 0 1 3 20zm3-6H18V12H6zm0 4h6V16H6z"/></svg>`;
const moreIcon = `<svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor"><circle cx="5" cy="12" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/></svg>`;

const editSelectedIcon = `
  <svg xmlns:xlink="http://www.w3.org/1999/xlink" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" aria-hidden="true" width="24" height="24">
    <path d="M20 14.66V20a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5.34" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
    <path d="M18 2 22 6 12 16 8 16 8 12 18 2" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
  </svg>
`;

const categorySelectedIcon = `
  <svg xmlns:xlink="http://www.w3.org/1999/xlink" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" aria-hidden="true" width="24" height="24">
    <path d="M17 10h2c2 0 3-1 3-3V5c0-2-1-3-3-3H17c-2 0-3 1-3 3V7c0 2 1 3 3 3z" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
    <path d="M5 22H7c2 0 3-1 3-3V17c0-2-1-3-3-3H5c-2 0-3 1-3 3v2c0 2 1 3 3 3z" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
    <path d="M6 10c2.209 0 4-1.791 4-4S8.209 2 6 2 2 3.791 2 6s1.791 4 4 4z" opacity="0.34" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
    <path d="M18 22c2.209 0 4-1.791 4-4s-1.791-4-4-4-4 1.791-4 4 1.791 4 4 4z" opacity="0.34" stroke="#F3F6FB" fill="none" stroke-width="1.8px"></path>
  </svg>
`;

const deleteSelectedIcon = `
  <svg viewBox="0 0 24 24" aria-hidden="true" width="24" height="24">
    <path d="M9 3.75h6" />
    <path d="M4.5 6.75h15" />
    <path d="M7.5 6.75v11.25c0 .828.672 1.5 1.5 1.5h6c.828 0 1.5-.672 1.5-1.5V6.75" />
    <path d="M10.5 10.5v5.25" />
    <path d="M13.5 10.5v5.25" />
  </svg>
`;

const descriptionIcon = `
  <svg fill="currentColor" viewBox="144 144 512 512" aria-hidden="true">
    <path d="m588.93 295.04H211.07v-62.977c0-5.5664 2.2109-10.906 6.1484-14.844s9.2773-6.1484 14.844-6.1484h335.87c5.5703 0 10.91 2.2109 14.844 6.1484 3.9375 3.9375 6.1484 9.2773 6.1484 14.844zM211.07 567.93V337.02h377.86v230.91c0 5.5703-2.2109 10.91-6.1484 14.844-3.9336 3.9375-9.2734 6.1484-14.844 6.1484h-335.87c-5.5664 0-10.906-2.2109-14.844-6.1484-3.9375-3.9336-6.1484-9.2734-6.1484-14.844zm62.977-125.95 251.9 0.003906v-41.984h-251.9zm0 83.969h125.95v-41.984h-125.95z" />
  </svg>
`;

function renderAuthenticatedBadge(): string {
  if (!state.tokenConfigured || !state.sessionUsername) {
    return "";
  }

  const showSelector = state.olxAvailableTokens.length > 1;
  const currentLabel = escapeHtml(state.sessionUsername);

  if (!showSelector) {
    return `
      <div class="session-badge olx-header-badge">
        <span>OLX</span>
        <strong>${currentLabel}</strong>
      </div>
    `;
  }

  return `
    <div class="olx-token-picker">
      <button
        class="session-badge olx-header-badge olx-token-trigger ${state.olxTokenMenuOpen ? "is-open" : ""}"
        id="olxTokenTrigger"
        type="button"
        aria-expanded="${state.olxTokenMenuOpen ? "true" : "false"}"
        ${state.olxTokenSwitching ? "disabled" : ""}
      >
        <span>OLX</span>
        <strong>${currentLabel}</strong>
        <svg viewBox="0 0 20 20" aria-hidden="true">
          <path d="M5.5 7.5 10 12l4.5-4.5" />
        </svg>
      </button>
      ${
        state.olxTokenMenuOpen
          ? `
            <div class="olx-token-menu" id="olxTokenMenu">
              ${state.olxAvailableTokens
                .map((tokenName) => {
                  const selected = tokenName === (state.olxProfileName || state.sessionUsername);
                  return `
                    <button
                      class="olx-token-option ${selected ? "is-active" : ""}"
                      type="button"
                      data-olx-token="${escapeHtml(tokenName)}"
                      ${state.olxTokenSwitching ? "disabled" : ""}
                    >
                      ${escapeHtml(tokenName)}
                    </button>
                  `;
                })
                .join("")}
            </div>
          `
          : ""
      }
    </div>
  `;
}

function render(): void {
  if (state.authChecking) {
    renderAuthLoading(root);
    return;
  }

  if (!state.isAuthenticated) {
    renderLoginScreen(root, state, async ({ username, password, rememberMe }) => {
      if (!username || !password) {
        state.authError = "";
        state.showFieldError = true;
        render();
        return;
      }

      state.authSubmitting = true;
      state.authError = "";
      state.showFieldError = false;
      state.rememberMe = rememberMe;
      render();

      try {
        await loginApp(username, password, rememberMe);
        state.showFieldError = false;
        await refreshAuthentication(true);
        await startApplication();
      } catch (error) {
        state.authSubmitting = false;
        state.authError = error instanceof Error ? error.message : "Login nije uspio.";
        state.showFieldError = true;
        render();
      }
    });
    return;
  }

  const publishSessionStats = getPublishSessionStats();

  if (isDescriptionPath()) {
    setDescriptionPageSession(
      state.sessionUsername,
      state.tokenConfigured,
      state.olxProfileName,
    );
    mountDescriptionPage(root);
    return;
  }

  if (isBulkCategoryPath()) {
    setBulkCategoryPageSession(state.sessionUsername, state.tokenConfigured);
    mountBulkCategoryPage(root);
    return;
  }

  if (isBulkEditPath()) {
    setBulkEditPageSession(state.sessionUsername, state.tokenConfigured);
    mountBulkEditPage(root);
    return;
  }

  const editTowKod = getEditTowKodFromPath();
  if (editTowKod) {
    setEditPageSession(state.sessionUsername, state.tokenConfigured);
    mountEditPage(root, editTowKod);
    return;
  }

  const categoryTowKod = getCategoryTowKodFromPath();
  if (categoryTowKod) {
    setCategoryPageSession(state.sessionUsername, state.tokenConfigured);
    mountCategoryPage(root, categoryTowKod);
    return;
  }

  const hasItems = state.data.items.length > 0;

  const sortedItems = sortItems(state.data.items, state.sortMode);

  const cards = sortedItems
    .map((item) => {
      const imageKey = item.itemId;
      const selectionKey = getItemSelectionKey(item);
      const isSelected = selectionKey
        ? state.selectedTowKods.includes(selectionKey)
        : false;
      const shouldPulse = selectionKey
        ? state.selectionPulseTowKods.includes(selectionKey)
        : false;
      const processing = state.data.aiProcessing.items[item.itemId];
		const displayImages = displayImagesForItem(item);
      const currentImageIndex = Math.min(
        state.imageIndexes[imageKey] ?? 0,
        Math.max(displayImages.length - 1, 0),
      );
      const image = displayImages[currentImageIndex]?.url ?? "";
      const price =
        item.retailPrice > 0
          ? `${formatPriceValue(item.retailPrice)} ${escapeHtml(formatCurrencyCode(item.currencyCode || "EUR"))}`
          : "Na upit";
      const hasCategorization = Boolean(item.categorization?.category?.id);

      return `
        <article class="product-card ${state.viewMode === "list" ? "list-mode" : ""} ${state.selectionMode ? "selection-enabled" : ""} ${isSelected ? "is-selected" : ""} ${shouldPulse ? "selection-pulse" : ""}" data-select-card="${escapeHtml(selectionKey)}">
          <div class="product-media" data-product-image-key="${escapeHtml(imageKey)}">
            ${
              isSelected
                ? `<div class="selection-badge">ODABRANO</div>`
                : ""
            }
            <div class="card-image-menu-shell">
              <button class="card-image-menu-trigger" type="button" data-image-menu-trigger="${escapeHtml(item.itemId)}" aria-label="Opcije slika" aria-expanded="false">${moreIcon}</button>
              <div class="card-image-menu" data-image-menu="${escapeHtml(item.itemId)}" hidden>
                <button type="button" data-image-action="append" data-image-item="${escapeHtml(item.itemId)}">Dodaj sliku</button>
                <button type="button" data-image-action="cover" data-image-item="${escapeHtml(item.itemId)}">Dodaj naslovnu sliku</button>
                <button type="button" data-image-action="edit" data-image-item="${escapeHtml(item.itemId)}">Promijeni redoslijed slika</button>
              </div>
            </div>
            ${
              image
                ? `<img src="${image}" alt="${escapeHtml(item.nameB2B)}" loading="lazy" draggable="false" />`
                : `<div class="product-placeholder">Nema slike</div>`
            }
            ${
              displayImages.length > 1
                ? `
                  <button class="image-arrow left" data-image-nav="prev" data-image-key="${escapeHtml(imageKey)}" type="button" aria-label="Prethodna slika">
                    <svg viewBox="0 0 20 20" aria-hidden="true">
                      <path d="M12.5 4.5 7 10l5.5 5.5" />
                    </svg>
                  </button>
                  <button class="image-arrow right" data-image-nav="next" data-image-key="${escapeHtml(imageKey)}" type="button" aria-label="Sledeca slika">
                    <svg viewBox="0 0 20 20" aria-hidden="true">
                      <path d="M7.5 4.5 13 10l-5.5 5.5" />
                    </svg>
                  </button>
                `
                : ""
            }
            ${
              displayImages.length > 0
				? `<div class="image-count" data-image-count-key="${escapeHtml(imageKey)}">${currentImageIndex + 1}/${displayImages.length}</div>`
                : ""
            }
            ${processing ? renderAIProcessingStatuses(processing) : ""}
          </div>
          <div class="product-body">
            <h3>${escapeHtml(item.nameB2B || "Artikal bez naziva")}</h3>
            <div class="product-line">
              <span>${escapeHtml(item.brandName || "Nepoznat brend")}</span>
              <span>${escapeHtml(item.index || item.towKod || "Bez sifre")}</span>
            </div>
            <div class="product-description-preview" data-description-scroll-key="${escapeHtml(item.itemId)}">${renderTechnicalDescription(item)}</div>
            <div class="product-line bottom">
              <strong>${price}</strong>
              <div class="card-actions">
                <button class="icon-button ${hasCategorization ? "category-button-complete" : "category-button-missing"}" type="button" aria-label="Kategorizacija" data-category-code="${escapeHtml(item.itemId)}">
                  ${categorizeIcon}
                </button>
                <button class="icon-button" type="button" aria-label="Promjeni" data-edit-code="${escapeHtml(item.itemId)}">
                  ${editIcon}
                </button>
              </div>
            </div>
			<div class="card-ai-actions">
				<button class="icon-button ai-card-button ${processing?.image.status === "done" ? "ai-card-done" : ""}" type="button" aria-label="Obradi sliku" title="Obradi sliku" data-ai-process="image" data-ai-item-id="${escapeHtml(item.itemId)}" ${processing?.image.status === "queued" || processing?.image.status === "processing" || processing?.image.status === "done" ? "disabled" : ""}>${processing?.image.status === "done" ? "✓" : aiImageIcon}</button>
				<button class="icon-button ai-card-button ${processing?.description.status === "done" ? "ai-card-done" : ""}" type="button" aria-label="Obradi opis" title="Obradi opis" data-ai-process="description" data-ai-item-id="${escapeHtml(item.itemId)}" ${processing?.description.status === "queued" || processing?.description.status === "processing" || processing?.description.status === "done" ? "disabled" : ""}>${processing?.description.status === "done" ? "✓" : aiDescriptionIcon}</button>
				<label class="ai-content-switch" title="Prikaži/koristi AI verziju"><input type="checkbox" data-ai-toggle="${escapeHtml(item.itemId)}" ${item.useAIContent ? "checked" : ""} ${!hasAIContent(item, processing) ? "disabled" : ""}/><span></span></label>
			</div>
          </div>
        </article>
      `;
    })
    .join("");

  const shouldRestoreDescriptionScroll = isMainPath();
  if (shouldRestoreDescriptionScroll) {
    saveDescriptionPreviewScrollPositions();
  }

  root.innerHTML = `
    <div class="shell">
      <header class="topbar">
        ${renderAppBrand(renderAuthenticatedBadge())}
        <div class="header-actions">
        ${
          state.selectionToolsVisible
            ? `
              <div class="selection-tools ${state.selectionToolsAnimation}">
				<div class="toolbar-action bulk-options-shell">
				  <button class="header-icon-button selected-action-button bulk-options-button" id="bulkOptionsButton" type="button" aria-label="Opcije za selektovane artikle" aria-expanded="${state.bulkOptionsOpen}"><span class="button-icon">${moreIcon}</span></button>
				  <span class="action-label action-label-toggle">OPCIJE</span>
                  <div class="bulk-options-menu" id="bulkOptionsMenu" ${state.bulkOptionsOpen ? "" : "hidden"}>
                    <button type="button" data-bulk-option="ai-image"><span>${aiImageIcon}</span>AI slike</button>
                    <button type="button" data-bulk-option="ai-description"><span>${aiDescriptionIcon}</span>AI opis</button>
                    <div class="bulk-options-divider"></div>
                    <strong>SLIKE</strong>
                    <button type="button" data-bulk-option="cover">Postavi naslovnu sliku</button>
                    <button type="button" data-bulk-option="append">Dodaj obične slike</button>
                  </div>
				</div>
				<div class="toolbar-action">
                  <button class="header-icon-button selected-action-button category-selected-button" id="categorySelectedButton" type="button" aria-label="Kategorija za selektovane artikle">
                    <span class="button-icon">${categorySelectedIcon}</span>
                  </button>
                  <span class="action-label action-label-toggle">KATEGORIJA</span>
                </div>
                <div class="toolbar-action">
                  <button class="header-icon-button selected-action-button edit-selected-button" id="editSelectedButton" type="button" aria-label="Promjena za selektovane artikle">
                    <span class="button-icon">${editSelectedIcon}</span>
                  </button>
                  <span class="action-label action-label-toggle">PROMJENA</span>
                </div>
                <div class="toolbar-action">
                  <button
                    class="header-icon-button selected-action-button delete-selected-button"
                    id="deleteSelectedButton"
                    type="button"
                    aria-label="Obrisi selektovane artikle"
                    ${state.isDeletingSelected ? "disabled" : ""}
                  >
                    <span class="button-icon">${deleteSelectedIcon}</span>
                  </button>
                  <span class="action-label action-label-toggle">DELETE</span>
                </div>
                <div class="toolbar-action">
                  <button
                    class="header-icon-button select-all-button ${state.selectAllMode ? "is-active" : "is-inactive"}"
                    id="selectAllButton"
                    type="button"
                    aria-label="${state.selectAllMode ? "Iskljuci sve" : "Ukljuci sve"}"
                    aria-pressed="${state.selectAllMode ? "true" : "false"}"
                  >
                    <span class="button-icon select-all-icon">
                      ${state.selectAllMode ? selectAllOnIcon : selectAllOffIcon}
                    </span>
                  </button>
                  <span class="action-label action-label-toggle">SVE</span>
                </div>
              </div>
            `
            : ""
        }
        <div class="toolbar-action">
          <button
            class="header-icon-button selection-button ${state.selectionMode ? "is-active" : "is-inactive"}"
            id="selectionButton"
            type="button"
            aria-label="${state.selectionMode ? "Iskljuci selekciju" : "Ukljuci selekciju"}"
            aria-pressed="${state.selectionMode ? "true" : "false"}"
          >
            <span class="button-icon selection-icon ${state.selectionAnimation}">
              ${state.selectionMode ? selectionOnIcon : selectionOffIcon}
            </span>
          </button>
          <span class="action-label action-label-toggle">SELEKCIJA</span>
        </div>
        <div class="toolbar-action">
          <button class="header-icon-button sort-button ${state.sortMenuOpen ? "is-open" : ""}" id="sortButton" type="button" aria-label="Sortiraj artikle" aria-expanded="${state.sortMenuOpen ? "true" : "false"}">
            <span class="button-icon sort-icon">${sortIcon}</span>
          </button>
          <span class="action-label action-label-toggle">SORTIRAJ</span>
          ${
            state.sortMenuOpen
              ? `
                <div class="sort-menu" id="sortMenu" role="menu" aria-label="Opcije sortiranja">
                  ${(
                    Object.entries(SORT_MODE_LABELS) as Array<[SortMode, string]>
                  )
                    .map(
                      ([mode, label]) => `
                        <button
                          class="sort-menu-item ${state.sortMode === mode ? "is-active" : ""}"
                          type="button"
                          role="menuitemradio"
                          aria-checked="${state.sortMode === mode ? "true" : "false"}"
                          data-sort-mode="${mode}"
                        >
                          ${label}
                        </button>
                      `,
                    )
                    .join("")}
                </div>
              `
              : ""
          }
        </div>
        <div class="toolbar-action">
          <button class="view-toggle-button" id="viewToggleButton" type="button" aria-label="${
            state.viewMode === "grid" ? "Prebaci na listu" : "Prebaci na kartice"
          }">
            <span class="button-icon view-toggle-icon">${
              state.viewMode === "grid" ? gridViewIcon : listViewIcon
            }</span>
          </button>
          <span class="action-label action-label-toggle">${
            state.viewMode === "grid" ? "KARTICE" : "LISTA"
          }</span>
        </div>
        <div class="toolbar-action">
          <button class="primary-button" id="uploadButton" ${
            state.isUploading ? "disabled" : ""
          }>
            <span class="button-icon">${plusIcon}</span>
          </button>
          <span class="action-label action-label-upload">DODAJ</span>
        </div>
        <div class="toolbar-action">
          <button class="header-icon-button logout-button" id="logoutButton" type="button" aria-label="Odjava">
            <span class="button-icon">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M10 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4" />
                <path d="M16 16l4-4-4-4" />
                <path d="M20 12H9" />
              </svg>
            </span>
          </button>
          <span class="action-label action-label-toggle">ODJAVA</span>
        </div>
        </div>
      </header>

      <main class="main-layout ${hasItems ? "has-items" : "is-empty"}">
        <section class="drop-layer ${state.isDragging ? "dragging" : ""}" id="dropZone">
        <input id="fileInput" type="file" accept=".json,.zip,application/json,application/zip" multiple hidden />

        ${
          hasItems
            ? `
              <section class="products-grid ${state.viewMode === "list" ? "list-view" : ""} ${state.animateItemsIn ? "products-grid-animated" : ""}">
                ${cards}
              </section>
            `
            : `
              <section class="drop-simple">
                <button class="drop-simple-button" id="browseButton" ${
                  state.isUploading ? "disabled" : ""
                }>
                  <span class="button-icon center-button-icon">${centerPlusIcon}</span>
                </button>
              </section>
            `
        }

        ${
          hasItems
            ? `
              <section class="drop-inline">
                <button class="drop-inline-button" id="browseButton" ${
                  state.isUploading ? "disabled" : ""
                }><span class="button-icon">${plusIcon}</span></button>
              </section>
            `
            : ""
        }
        </section>
      </main>

      <footer class="footer-bar">
        <div class="footer-actions-left">
          <div class="toolbar-action footer-clear-action">
            <button class="danger-button" id="clearButton">
              <span class="button-icon">${deleteIcon}</span>
            </button>
            <span class="action-label">OBRISI</span>
          </div>
          <div class="toolbar-action footer-save-action">
            <button
              class="save-footer-button"
              id="saveButton"
              type="button"
              ${state.data.items.length === 0 || hasBlockingAIForCurrentSelection() ? "disabled" : ""}
            >
              <span class="button-icon">${saveIcon}</span>
            </button>
            <span class="action-label">SACUVAJ</span>
          </div>
          <div class="footer-image-processing-control">
            <div class="footer-image-processing-meta" aria-live="polite">
              <span class="ai-runtime-indicator ${state.data.aiProcessing.aiReady ? "ready" : "unavailable"}" title="${escapeHtml(state.data.aiProcessing.aiError || "Lokalni AI za slike i opise je spreman")}" aria-label="${state.data.aiProcessing.aiReady ? "AI spreman" : "AI nije spreman"}"><span class="ai-runtime-status" aria-hidden="true">${state.data.aiProcessing.aiReady ? "✓" : "×"}</span><span>AI</span></span>
              ${
                state.data.totalItems > 0
                  ? renderAIProgressCounters()
                  : ""
              }
            </div>
          </div>
        </div>
        <div class="footer-counts" aria-live="polite">
          <span class="footer-count-total">${state.data.totalItems}</span>
          <span class="footer-count-selected">${state.selectedTowKods.length}</span>
        </div>
        <div class="footer-actions-right">
          <div class="toolbar-action">
            <button class="description-button" id="descriptionButton" type="button">
              <span class="button-icon description-button-icon">${descriptionIcon}</span>
            </button>
            <span class="action-label description-action-label">DESKRIPCIJA</span>
          </div>
          <div class="toolbar-action">
            <button class="publish-button" id="publishButton" type="button" ${
              isPublishBlocked() || hasBlockingAIForCurrentSelection() ? "disabled" : ""
            }>
              <span class="button-icon">${publishIcon}</span>
            </button>
            <span class="action-label">OBJAVI</span>
          </div>
          <div class="footer-publish-metrics" aria-live="polite">
            ${
              state.tokenConfigured && (state.listingLimitRemaining !== null || state.listingLimitLoading)
                ? `<span class="footer-publish-limit${state.listingLimitLoading && state.listingLimitRemaining === null ? " is-loading" : ""}">${state.listingLimitUnlimited ? "∞" : state.listingLimitRemaining ?? "..."}</span>`
                : ""
            }
            ${
              publishSessionStats.successCount > 0
                ? `<span class="footer-publish-success">${publishSessionStats.successCount}</span>`
                : ""
            }
            ${
              publishSessionStats.failedCount > 0
                ? `<span class="footer-publish-failed">${publishSessionStats.failedCount}</span>`
                : ""
            }
          </div>
        </div>
      </footer>
    </div>
	`;

  bindEvents();
  if (shouldRestoreDescriptionScroll) {
    restoreDescriptionPreviewScrollPositions();
  }
}

function bindEvents(): void {
  const fileInput = document.querySelector<HTMLInputElement>("#fileInput");
  const uploadButton =
    document.querySelector<HTMLButtonElement>("#uploadButton");
  const viewToggleButton =
    document.querySelector<HTMLButtonElement>("#viewToggleButton");
  const sortButton = document.querySelector<HTMLButtonElement>("#sortButton");
  const selectionButton =
    document.querySelector<HTMLButtonElement>("#selectionButton");
  const selectAllButton =
    document.querySelector<HTMLButtonElement>("#selectAllButton");
  const categorySelectedButton =
    document.querySelector<HTMLButtonElement>("#categorySelectedButton");
  const editSelectedButton =
    document.querySelector<HTMLButtonElement>("#editSelectedButton");
  const deleteSelectedButton =
    document.querySelector<HTMLButtonElement>("#deleteSelectedButton");
  const bulkOptionsButton = document.querySelector<HTMLButtonElement>("#bulkOptionsButton");
  const bulkOptionsMenu = document.querySelector<HTMLElement>("#bulkOptionsMenu");
  const bulkOptionButtons = document.querySelectorAll<HTMLButtonElement>("[data-bulk-option]");
  const imageMenuTriggers = document.querySelectorAll<HTMLButtonElement>("[data-image-menu-trigger]");
  const imageActionButtons = document.querySelectorAll<HTMLButtonElement>("[data-image-action]");
  const selectableCards =
    document.querySelectorAll<HTMLElement>("[data-select-card]");
  const sortMenu = document.querySelector<HTMLElement>("#sortMenu");
  const sortOptionButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-sort-mode]");
  const browseButton =
    document.querySelector<HTMLButtonElement>("#browseButton");
  const dropZone = document.querySelector<HTMLElement>("#dropZone");
  const clearButton = document.querySelector<HTMLButtonElement>("#clearButton");
  const saveButton = document.querySelector<HTMLButtonElement>("#saveButton");
  const descriptionButton =
    document.querySelector<HTMLButtonElement>("#descriptionButton");
  const publishButton =
    document.querySelector<HTMLButtonElement>("#publishButton");
  const logoutButton =
    document.querySelector<HTMLButtonElement>("#logoutButton");
	const aiProcessButtons = document.querySelectorAll<HTMLButtonElement>("[data-ai-process]");
	const aiToggleInputs = document.querySelectorAll<HTMLInputElement>("[data-ai-toggle]");
  const olxTokenTrigger =
    document.querySelector<HTMLButtonElement>("#olxTokenTrigger");
  const olxTokenMenu =
    document.querySelector<HTMLElement>("#olxTokenMenu");
  const olxTokenOptions =
    document.querySelectorAll<HTMLButtonElement>("[data-olx-token]");
  const imageButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-image-nav]");
  const editButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-edit-code]");
  const categoryButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-category-code]");

  if (!fileInput || !uploadButton || !clearButton) {
    return;
  }

  logoutButton?.addEventListener("click", async () => {
    await forceLogoutToLogin();
  });

	bulkOptionsButton?.addEventListener("click", (event) => {
    event.stopPropagation();
    state.bulkOptionsOpen = !state.bulkOptionsOpen;
    render();
  });
  bulkOptionsMenu?.addEventListener("click", (event) => event.stopPropagation());
  for (const button of bulkOptionButtons) {
    button.addEventListener("click", async () => {
      const option = button.dataset.bulkOption;
      state.bulkOptionsOpen = false;
      if (state.selectedTowKods.length === 0) {
        state.message = "Odaberi barem jedan artikal.";
        state.messageTone = "danger";
        render();
        return;
      }
      if (option === "ai-image" || option === "ai-description") {
        await requestAIProcess(state.selectedTowKods, option === "ai-image" ? "image" : "description");
        return;
      }
      if (option !== "append" && option !== "cover") return;
      if (bulkOptionsMenu) bulkOptionsMenu.hidden = true;
      try {
        const result = await pickAndUploadBulkImages<CatalogState>(state.selectedTowKods, option);
        if (!result) {
          render();
          return;
        }
        state.data = result.state;
        state.imageIndexes = {};
        state.message = option === "cover"
          ? "Naslovna slika je postavljena selektovanim artiklima."
          : "Slike su dodane selektovanim artiklima.";
        if (result.skipped > 0) state.message += ` Preskočeno duplikata: ${result.skipped}.`;
        state.messageTone = "success";
      } catch (error) {
        state.message = error instanceof Error ? error.message : "Slike nisu dodane.";
        state.messageTone = "danger";
      }
      render();
    });
  }
	for (const trigger of imageMenuTriggers) {
    trigger.addEventListener("click", (event) => {
      event.stopPropagation();
      const itemID = trigger.dataset.imageMenuTrigger || "";
      const menu = Array.from(document.querySelectorAll<HTMLElement>("[data-image-menu]")).find((candidate) => candidate.dataset.imageMenu === itemID);
      const willOpen = Boolean(menu?.hidden);
      document.querySelectorAll<HTMLElement>("[data-image-menu]").forEach((candidate) => { candidate.hidden = true; });
      if (menu) menu.hidden = !willOpen;
      for (const candidate of imageMenuTriggers) {
        candidate.setAttribute("aria-expanded", String(candidate === trigger && willOpen));
      }
      if (willOpen) {
        window.setTimeout(() => {
          document.addEventListener("click", () => {
            document.querySelectorAll<HTMLElement>("[data-image-menu]").forEach((candidate) => { candidate.hidden = true; });
            document.querySelectorAll<HTMLButtonElement>("[data-image-menu-trigger]").forEach((candidate) => candidate.setAttribute("aria-expanded", "false"));
          }, { once: true });
        }, 0);
      }
    });
  }
  for (const button of imageActionButtons) {
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      const item = state.data.items.find((candidate) => candidate.itemId === button.dataset.imageItem);
      if (!item) return;
      document.querySelectorAll<HTMLElement>("[data-image-menu]").forEach((candidate) => { candidate.hidden = true; });
      openImageManager(item, (updated) => {
        const index = state.data.items.findIndex((candidate) => candidate.itemId === updated.itemId);
        if (index >= 0) state.data.items[index] = updated as CatalogItem;
        state.imageIndexes[updated.itemId] = 0;
        state.message = "Galerija slika je sačuvana.";
        state.messageTone = "success";
        render();
      }, (button.dataset.imageAction as "edit" | "append" | "cover") || "edit");
    });
  }
	for (const button of aiProcessButtons) button.addEventListener("click", () => {
		const itemID = button.dataset.aiItemId; const kind = button.dataset.aiProcess as "image" | "description";
		if (itemID && (kind === "image" || kind === "description")) void requestAIProcess([itemID], kind);
	});
	for (const input of aiToggleInputs) input.addEventListener("change", () => {
		const itemID = input.dataset.aiToggle; if (itemID) void setAIContentToggle(itemID, input.checked);
	});


  olxTokenTrigger?.addEventListener("click", (event) => {
    event.stopPropagation();
    state.olxTokenMenuOpen = !state.olxTokenMenuOpen;
    if (state.olxTokenMenuOpen) {
      window.setTimeout(() => {
        document.addEventListener("click", handleOlxTokenMenuOutsideClick, {
          once: true,
        });
      }, 0);
    }
    render();
  });

  olxTokenMenu?.addEventListener("click", (event) => {
    event.stopPropagation();
  });

  for (const button of olxTokenOptions) {
    button.addEventListener("click", async () => {
      const tokenName = button.dataset.olxToken;
      if (!tokenName || state.olxTokenSwitching) {
        return;
      }

      state.olxTokenSwitching = true;
      state.olxTokenMenuOpen = false;
      render();

      try {
        const session = await switchOlxToken(tokenName);
        state.sessionUsername = session.username || "";
        state.tokenConfigured = session.tokenConfigured;
        state.olxProfileName = session.profileName || "";
        state.olxAvailableTokens = session.availableTokens || [];
        await refreshListingLimits();
        render();
      } catch (error) {
        state.message =
          error instanceof Error ? error.message : "Promjena OLX tokena nije uspjela.";
        state.messageTone = "danger";
        render();
      } finally {
        state.olxTokenSwitching = false;
        render();
      }
    });
  }

  const openPicker = () => {
    suspendMainRefresh(3000);
    state.isPickingFiles = true;
    fileInput.click();
  };
  uploadButton.addEventListener("click", openPicker);
  sortButton?.addEventListener("click", (event) => {
    event.stopPropagation();
    state.sortMenuOpen = !state.sortMenuOpen;
    if (state.sortMenuOpen) {
      window.setTimeout(() => {
        document.addEventListener("click", handleSortMenuOutsideClick, {
          once: true,
        });
      }, 0);
    }
    render();
  });
  selectionButton?.addEventListener("click", () => {
    const nextSelectionMode = !state.selectionMode;
    state.selectionMode = nextSelectionMode;
    state.selectionAnimation = state.selectionMode ? "animate-on" : "animate-off";
    saveStoredSelectionMode(state.selectionMode);
    if (nextSelectionMode) {
      reconcileSelectionState();
      state.selectionToolsVisible = true;
      state.selectionToolsAnimation = "selection-tools-enter";
    } else {
      state.selectionToolsAnimation = "selection-tools-exit";
      state.selectAllMode = false;
      state.selectedTowKods = [];
      state.selectionPulseTowKods = [];
      saveStoredSelectAllMode(false);
      saveStoredSelectedTowKods([]);
      window.setTimeout(() => {
        state.selectionToolsVisible = false;
        state.selectionToolsAnimation = "";
        render();
      }, 280);
    }
    render();
    window.setTimeout(() => {
      state.selectionAnimation = "";
      if (state.selectionToolsAnimation === "selection-tools-enter") {
        state.selectionToolsAnimation = "";
      }
      render();
    }, 320);
  });
  selectAllButton?.addEventListener("click", (event) => {
    event.stopPropagation();
    const selectableTowKods = getSelectableTowKods(state.data.items);
    if (selectableTowKods.length === 0) {
      state.selectAllMode = false;
      state.selectedTowKods = [];
      saveStoredSelectAllMode(false);
      saveStoredSelectedTowKods([]);
      render();
      return;
    }

    const shouldSelectAll = !state.selectAllMode;
    const previouslySelected = new Set(state.selectedTowKods);
    state.selectAllMode = shouldSelectAll;
    state.selectedTowKods = shouldSelectAll ? selectableTowKods : [];
    state.selectionPulseTowKods = shouldSelectAll
      ? selectableTowKods.filter((towKod) => !previouslySelected.has(towKod))
      : [];
    saveStoredSelectAllMode(state.selectAllMode);
    saveStoredSelectedTowKods(state.selectedTowKods);
    render();
    if (state.selectionPulseTowKods.length > 0) {
      window.setTimeout(() => {
        state.selectionPulseTowKods = [];
        render();
      }, 240);
    }
  });
  editSelectedButton?.addEventListener("click", () => {
    if (state.selectedTowKods.length === 0) {
      return;
    }

    saveMainScrollPosition();
    navigateWithFade(root, "/edit-selected");
  });
  categorySelectedButton?.addEventListener("click", () => {
    if (state.selectedTowKods.length === 0) {
      return;
    }

    saveMainScrollPosition();
    navigateWithFade(root, "/category-selected");
  });
  deleteSelectedButton?.addEventListener("click", async () => {
    if (state.selectedTowKods.length === 0 || state.isDeletingSelected) {
      return;
    }

    state.isDeletingSelected = true;
    state.message = "Brisem selektovane artikle...";
    state.messageTone = "muted";
    render();

    try {
      const response = await fetch("/api/catalog/bulk-delete", {
        method: "DELETE",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          itemKeys: state.selectedTowKods,
        }),
      });

      const data = (await response.json()) as CatalogResponse;
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Brisanje nije uspelo.");
      }

      state.data = data as CatalogState;
      state.imageIndexes = filterImageIndexesByItems(state.imageIndexes, state.data.items);
      state.selectedTowKods = [];
      state.selectionPulseTowKods = [];
      state.selectAllMode = false;
      saveStoredSelectedTowKods([]);
      saveStoredSelectAllMode(false);
      state.message = "Selektovani artikli su obrisani.";
      state.messageTone = "success";
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Brisanje nije uspelo.";
      state.messageTone = "danger";
    } finally {
      state.isDeletingSelected = false;
      render();
    }
  });
  descriptionButton?.addEventListener("click", () => {
    saveMainScrollPosition();
    navigateWithFade(root, "/description");
  });

  saveButton?.addEventListener("click", async () => {
    if (state.data.items.length === 0) {
      return;
    }

    try {
      const exportLabel =
        state.selectedTowKods.length > 0 ? "Cuvam selektovane artikle..." : "Cuvam sve artikle...";
      state.message = exportLabel;
      state.messageTone = "muted";
      render();

      await exportCatalogArchive();
      state.message = "Katalog je sacuvan.";
      state.messageTone = "success";
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") {
        state.message = "Cuvanje je otkazano.";
        state.messageTone = "muted";
        render();
        return;
      }
      state.message =
        error instanceof Error ? error.message : "Cuvanje nije uspelo.";
      state.messageTone = "danger";
    }

    render();
  });

  publishButton?.addEventListener("click", async () => {
    if (state.data.items.length === 0) {
      return;
    }

    const hasAnyCategorizedItem = state.data.items.some((item) =>
      Boolean(item.categorization?.category?.id),
    );
    if (!hasAnyCategorizedItem) {
      state.message = "Odaberite kategoriju artiklima!";
      state.messageTone = "danger";
      notyf.error("Odaberite kategoriju artiklima!");
      render();
      return;
    }

    if (isPublishBlocked()) {
      state.message = "Dostignut je other listing limit. Objava je blokirana dok se limit ne oslobodi.";
      state.messageTone = "danger";
      render();
      return;
    }

    const publishTowKods = getPublishCandidateTowKods();
    if (publishTowKods.length === 0) {
      state.message = "Nema artikala za objavu.";
      state.messageTone = "danger";
      showPublishErrorToast(0);
      render();
      return;
    }

    const optimisticSnapshot = createPublishOptimisticSnapshot();

    console.log("[PUBLISH] click", {
      selectedTowKods: state.selectedTowKods,
      totalItems: state.data.items.length,
      selectedItems: state.data.items.filter((item) =>
        state.selectedTowKods.includes(getItemSelectionKey(item)),
      ),
      categorizableItems: state.data.items.filter(
        (item) => Boolean(item.categorization?.category?.id),
      ),
    });

    state.activePublishCount += 1;
    state.message =
      state.selectedTowKods.length > 0
        ? "Dodajem selektovane artikle u red za objavu..."
        : "Dodajem artikle u red za objavu...";
    state.messageTone = "muted";
    applyOptimisticPublishRemoval(publishTowKods);
    render();

    try {
      await refreshListingLimits();
      if (isPublishBlocked()) {
        restorePublishSnapshot(optimisticSnapshot);
        state.message = "Dostignut je other listing limit. Objava je blokirana dok se limit ne oslobodi.";
        state.messageTone = "danger";
        showPublishErrorToast(publishTowKods.length);
        render();
        return;
      }

      const requestBody = {
        selectedItemKeys: publishTowKods,
        selectedTowKods: state.data.items
          .filter((item) => publishTowKods.includes(getItemSelectionKey(item)))
          .map((item) => item.towKod),
      };
      console.log("[PUBLISH] request body", requestBody);

      const response = await fetch("/api/catalog/publish", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(requestBody),
      });

      const data = (await response.json()) as PublishResponse | ErrorResponse;
      console.log("[PUBLISH] response", {
        ok: response.ok,
        status: response.status,
        data,
      });
      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Objava nije uspela.");
      }

      const publishData = data as PublishResponse;
      if (publishData.listingLimits) {
        applyListingLimits(publishData.listingLimits);
      } else if (state.listingLimitRemaining !== null && !state.listingLimitUnlimited) {
        state.listingLimitRemaining = Math.max(
          0,
          state.listingLimitRemaining - publishData.published.length,
        );
      }
      recordPublishSessionOutcome(publishData.published.length, publishData.failed.length);
      await syncCatalogStateFromBackend();

      const summaryParts = [
        `Objavljeno ${publishData.published.length}`,
        `preskoceno ${publishData.skipped.length}`,
        `gresaka ${publishData.failed.length}`,
      ];
      const primaryMessage =
        publishData.failed[0]?.error ||
        publishData.skipped[0]?.reason ||
        publishData.message;
      const dailyLimitMessage = extractDailyListingLimitMessage(primaryMessage);
      state.message = `${summaryParts.join(" | ")}${primaryMessage ? ` | ${primaryMessage}` : ""}`;
      state.messageTone =
        publishData.failed.length > 0
          ? "danger"
          : publishData.published.length > 0
            ? "success"
            : "muted";
      if (dailyLimitMessage) {
        showDailyLimitWarningToast(dailyLimitMessage);
      }
      showPublishOutcomeToasts(publishData.published.length, publishData.failed.length);
    } catch (error) {
      console.error("[PUBLISH] frontend error", error);
      const restored = await reloadStateAfterPublishFailure(optimisticSnapshot);
      const dailyLimitMessage = extractDailyListingLimitMessage(error);
      if (!restored) {
        state.message =
          dailyLimitMessage ||
          (error instanceof Error ? error.message : "Objava nije uspela.");
        state.messageTone = dailyLimitMessage ? "muted" : "danger";
      }
      if (dailyLimitMessage) {
        showDailyLimitWarningToast(dailyLimitMessage);
      } else {
        showPublishErrorToast(publishTowKods.length);
      }
    } finally {
      state.activePublishCount = Math.max(0, state.activePublishCount - 1);
      render();
    }
  });
  for (const card of selectableCards) {
    card.addEventListener("click", (event) => {
      if (!state.selectionMode) {
        return;
      }

      const target = event.target;
      if (
        target instanceof Element &&
        target.closest("button, a, input, textarea, select, label")
      ) {
        return;
      }

      const towKod = card.dataset.selectCard;
      if (!towKod) {
        return;
      }

      toggleTowKodSelection(towKod);
      render();
    });
  }
  viewToggleButton?.addEventListener("click", () => {
    state.viewMode = state.viewMode === "grid" ? "list" : "grid";
    saveViewMode(state.viewMode);
    render();
  });
  sortMenu?.addEventListener("click", (event) => {
    event.stopPropagation();
  });
  browseButton?.addEventListener("click", openPicker);

  for (const button of sortOptionButtons) {
    button.addEventListener("click", () => {
      const nextSortMode = button.dataset.sortMode as SortMode | undefined;
      if (!nextSortMode) {
        return;
      }

      state.sortMode = nextSortMode;
      state.sortMenuOpen = false;
      saveSortMode(nextSortMode);
      render();
    });
  }

  fileInput.addEventListener("change", async () => {
    const files = fileInput.files ? Array.from(fileInput.files) : [];
    if (files.length > 0) {
      suspendMainRefresh(3000);
    } else {
      suspendMainRefresh(600);
    }
    state.isPickingFiles = false;
    await uploadFiles(files);
    fileInput.value = "";
  });

  if (dropZone) {
    dropZone.addEventListener("dragenter", (event) => {
      event.preventDefault();
      state.isDragging = true;
      render();
    });

    dropZone.addEventListener("dragover", (event) => {
      event.preventDefault();
    });

    dropZone.addEventListener("dragleave", (event) => {
      event.preventDefault();
      const target = event.relatedTarget;
      if (!(target instanceof Node) || !dropZone.contains(target)) {
        state.isDragging = false;
        render();
      }
    });

    dropZone.addEventListener("drop", async (event) => {
      event.preventDefault();
      state.isDragging = false;
      render();
      const files = Array.from(event.dataTransfer?.files ?? []);
      await uploadFiles(files);
    });
  }

  clearButton.addEventListener("click", async () => {
    clearButton.disabled = true;
    suspendMainRefresh(1500);
    try {
      const response = await fetch("/api/catalog", { method: "DELETE" });
      const data = (await response.json()) as CatalogResponse;

      if (!response.ok) {
        throw new Error("error" in data ? data.error : "Brisanje nije uspelo.");
      }

      state.data = data as CatalogState;
      state.imageIndexes = {};
      reconcileSelectionState();
      state.message = "Sve je obrisano.";
      state.messageTone = "success";
    } catch (error) {
      state.message =
        error instanceof Error ? error.message : "Brisanje nije uspelo.";
      state.messageTone = "danger";
    } finally {
      clearButton.disabled = false;
      render();
    }
  });

  for (const button of imageButtons) {
    button.addEventListener("click", () => {
      const direction = button.dataset.imageNav;
      const imageKey = button.dataset.imageKey;

      if (!direction || !imageKey) {
        return;
      }

      const item = state.data.items.find((entry) => entry.itemId === imageKey);
      const displayImages = item
        ? displayImagesForItem(item)
        : [];

      if (!item || displayImages.length < 2) {
        return;
      }

      const currentIndex = state.imageIndexes[imageKey] ?? 0;
      const delta = direction === "next" ? 1 : -1;
      const nextIndex =
        (currentIndex + delta + displayImages.length) % displayImages.length;
      state.imageIndexes[imageKey] = nextIndex;
		updateProductCardImage(item, imageKey, displayImages, nextIndex);
    });
  }

  for (const button of editButtons) {
    button.addEventListener("click", () => {
      const code = button.dataset.editCode;
      if (!code) {
        return;
      }

      saveMainScrollPosition();
      navigateWithFade(root, `/edit/${encodeURIComponent(code)}`);
    });
  }

  for (const button of categoryButtons) {
    const prefetch = () => {
      const code = button.dataset.categoryCode;
      if (!code) {
        return;
      }

      void prefetchCategoryPage(code);
    };

    button.addEventListener("mouseenter", prefetch);
    button.addEventListener("focus", prefetch);
    button.addEventListener("click", async () => {
      const code = button.dataset.categoryCode;
      if (!code) {
        return;
      }

      await prefetchCategoryPage(code);
      saveMainScrollPosition();
      navigateWithFade(root, `/category/${encodeURIComponent(code)}`);
    });
  }

  if (state.shouldRestoreMainScroll) {
    restoreMainScrollPosition();
  }
}

async function loadState(backgroundRefresh = false): Promise<void> {
	if (loadStateInFlight) {
		return loadStateInFlight;
	}

	loadStateInFlight = loadStateOnce(backgroundRefresh).finally(() => {
		loadStateInFlight = null;
	});
	return loadStateInFlight;
}

async function loadStateOnce(backgroundRefresh = false): Promise<void> {
  let shouldRender = !backgroundRefresh;
  try {
    const session = await loadSessionInfo();
    state.sessionUsername = session.username || "";
    state.tokenConfigured = session.tokenConfigured;
    state.isAuthenticated = session.authenticated === true;
    state.appUsername = session.appUsername || "";
    state.appRole = session.appRole || "";
    state.olxProfileName = session.profileName || "";
    state.olxAvailableTokens = session.availableTokens || [];

    if (!state.isAuthenticated) {
      await forceLogoutToLogin("Morate se ponovo prijaviti.");
      return;
    }

    const previousLimitState = `${state.listingLimitRemaining}|${state.listingLimitUnlimited}`;
    const previousCatalogState = JSON.stringify(state.data);
    state.listingLimitLoading = state.tokenConfigured;

    const limitsPromise = state.tokenConfigured
      ? fetchCurrentListingLimits()
      : Promise.resolve<ListingLimits | null>(null);

    const response = await fetch("/api/catalog");
    if (response.status === 401) {
      await forceLogoutToLogin("Session je istekao. Prijavite se ponovo.");
      return;
    }
    const data = (await response.json()) as CatalogResponse;

    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    const nextData = data as CatalogState;
    applyListingLimits(await limitsPromise);
    const catalogChanged = previousCatalogState !== JSON.stringify(nextData);
    const limitsChanged = previousLimitState !== `${state.listingLimitRemaining}|${state.listingLimitUnlimited}`;
    if (
      !state.isUploading &&
      !state.isPickingFiles &&
      !isMainRefreshSuspended()
    ) {
      state.data = nextData;
      state.imageIndexes = {};
      reconcileSelectionState();
    }
    if (catalogChanged || limitsChanged) {
      shouldRender = true;
    }
    if (
      !state.isUploading &&
      !state.isPickingFiles &&
      !isMainRefreshSuspended() &&
      state.data.totalItems > 0
    ) {
      state.message = "Artikli su ucitani.";
      state.messageTone = "success";
    }

    if (state.shouldRestoreMainScroll) {
      restoreMainScrollPosition();
    }
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Backend nije dostupan.";
    state.messageTone = "danger";
  } finally {
    state.listingLimitLoading = false;
    if (shouldRender) {
      render();
    }
  }
}

async function refreshAuthentication(force = false): Promise<void> {
  state.authChecking = true;
  state.authSubmitting = false;

  if (force) {
    resetSessionInfoCache();
  }

  try {
    const session = await loadSessionInfo();
    state.isAuthenticated = session.authenticated === true;
    state.sessionUsername = session.username || "";
    state.tokenConfigured = session.tokenConfigured;
    state.appUsername = session.appUsername || "";
    state.appRole = session.appRole || "";
    state.olxProfileName = session.profileName || "";
    state.olxAvailableTokens = session.availableTokens || [];
  } catch {
    logClientEvent({
      category: "auth",
      level: "warn",
      message: "refresh authentication failed",
      details: {
        force,
      },
    });
    state.isAuthenticated = false;
    state.sessionUsername = "";
    state.tokenConfigured = false;
    state.appUsername = "";
    state.appRole = "";
    state.olxProfileName = "";
    state.olxAvailableTokens = [];
  } finally {
    state.authChecking = false;
  }
}

function startAuthPolling(): void {
  stopAuthPolling();

  authPollTimer = window.setInterval(() => {
    if (
      !state.isAuthenticated ||
      state.authChecking ||
      state.authSubmitting ||
      shouldPauseAuthPollingForCurrentPath()
    ) {
      return;
    }

    void refreshAuthentication(true).then(async () => {
      if (!state.isAuthenticated) {
        await forceLogoutToLogin("Login podaci su promijenjeni. Prijavite se ponovo.");
        return;
      }

      if (isMainPath()) {
        render();
      }
    });
  }, AUTH_POLL_INTERVAL_MS);
}

function stopAuthPolling(): void {
  if (authPollTimer === null) {
    return;
  }

  window.clearInterval(authPollTimer);
  authPollTimer = null;
}

function resetProtectedState(): void {
  state.data = {
    catalogs: [],
    items: [],
    tempDir: "",
    totalFiles: 0,
    totalItems: 0,
    lastUpdated: null,
    imageProcessing: { enabled: false, aiReady: false, items: {}, queued: 0, working: 0, done: 0, failed: 0 },
    aiProcessing: { enabled: false, aiReady: false, items: {}, queued: 0, working: 0, done: 0, failed: 0 },
  };
  state.imageIndexes = {};
  state.selectedTowKods = [];
  state.selectionPulseTowKods = [];
  state.selectAllMode = false;
  state.activePublishCount = 0;
  state.isUploading = false;
  state.isPickingFiles = false;
  state.listingLimitRemaining = null;
  state.listingLimitUnlimited = false;
  state.listingLimitLoading = false;
  state.sessionUsername = "";
  state.tokenConfigured = false;
  state.appUsername = "";
  state.appRole = "";
  state.olxProfileName = "";
  state.olxAvailableTokens = [];
  state.olxTokenMenuOpen = false;
  state.olxTokenSwitching = false;
}

async function forceLogoutToLogin(message = ""): Promise<void> {
  logClientEvent({
    category: "auth",
    level: "warn",
    message: "forcing logout to login screen",
    details: {
      message,
    },
  });
  stopAuthPolling();

  try {
    await logoutApp();
  } catch {
    resetSessionInfoCache();
  }

  resetProtectedState();
  state.isAuthenticated = false;
  state.authChecking = false;
  state.authSubmitting = false;
  state.authError = message;
  state.showFieldError = false;
  if (!isMainPath()) {
    window.history.replaceState({}, "", "/");
  }
  render();
}

async function startApplication(): Promise<void> {
  logClientEvent({
    category: "app",
    message: "application start requested",
    details: {
      authenticated: state.isAuthenticated,
      path: window.location.pathname,
    },
  });
  state.authSubmitting = false;
  state.authChecking = false;

  if (!state.isAuthenticated) {
    stopAuthPolling();
    render();
    return;
  }

  startAuthPolling();

  if (
    isDescriptionPath() ||
    isBulkCategoryPath() ||
    isBulkEditPath() ||
    Boolean(getEditTowKodFromPath()) ||
    Boolean(getCategoryTowKodFromPath())
  ) {
    render();
    return;
  }

  await loadState();
  logClientEvent({
    category: "app",
    message: "application started",
    details: {
      items: state.data.totalItems,
      path: window.location.pathname,
    },
  });
}

async function refreshListingLimits(): Promise<void> {
  if (!state.tokenConfigured) {
    applyListingLimits(null);
    state.listingLimitLoading = false;
    return;
  }

  state.listingLimitLoading = true;

  try {
    const limits = await fetchCurrentListingLimits();
    applyListingLimits(limits);
  } finally {
    state.listingLimitLoading = false;
  }
}

async function fetchCurrentListingLimits(): Promise<ListingLimits | null> {
  try {
    return await fetchListingLimits();
  } catch (error) {
    console.error("[LIMITS] fetch failed", error);
    return null;
  }
}

function applyListingLimits(limits: ListingLimits | null): void {
  if (!limits || !limits.tokenConfigured) {
    state.listingLimitRemaining = null;
    state.listingLimitUnlimited = false;
    return;
  }

  state.listingLimitUnlimited = limits.unlimited;
  state.listingLimitRemaining = limits.unlimited ? 0 : Math.max(0, limits.remaining);
}

function isPublishBlocked(): boolean {
  return (
    state.tokenConfigured &&
    !state.listingLimitUnlimited &&
    state.listingLimitRemaining !== null &&
    state.listingLimitRemaining <= 0
  );
}

function refreshMainPageFromStore(backgroundRefresh = false): void {
  if (
    shouldPauseAuthPollingForCurrentPath() ||
    state.isUploading ||
    state.isPickingFiles ||
    isMainRefreshSuspended()
  ) {
    return;
  }

  void loadState(backgroundRefresh);
}

function saveDescriptionPreviewScrollPositions(): void {
  for (const preview of document.querySelectorAll<HTMLElement>("[data-description-scroll-key]")) {
    const key = preview.dataset.descriptionScrollKey;
    if (key) {
      descriptionPreviewScrollPositions.set(key, preview.scrollTop);
    }
  }
}

function restoreDescriptionPreviewScrollPositions(): void {
  window.requestAnimationFrame(() => {
    for (const preview of document.querySelectorAll<HTMLElement>("[data-description-scroll-key]")) {
      const key = preview.dataset.descriptionScrollKey;
      const scrollTop = key ? descriptionPreviewScrollPositions.get(key) : undefined;
      if (scrollTop !== undefined) {
        preview.scrollTop = scrollTop;
      }
    }
  });
}

function saveMainScrollPosition(): void {
  if (!isMainPath()) {
    return;
  }

  window.sessionStorage.setItem(
    MAIN_SCROLL_TOP_STORAGE_KEY,
    String(Math.max(0, Math.round(window.scrollY))),
  );
}

function restoreMainScrollPosition(): void {
  const rawValue = window.sessionStorage.getItem(MAIN_SCROLL_TOP_STORAGE_KEY);
  const scrollTop = Number(rawValue);

  state.shouldRestoreMainScroll = false;

  if (!Number.isFinite(scrollTop) || scrollTop <= 0) {
    return;
  }

  window.requestAnimationFrame(() => {
    window.scrollTo({ top: scrollTop, behavior: "auto" });
  });
}

function suspendMainRefresh(durationMs: number): void {
  const nextSuspendUntil = Date.now() + Math.max(0, durationMs);
  state.refreshSuspendUntil = Math.max(state.refreshSuspendUntil, nextSuspendUntil);
}

function isMainRefreshSuspended(): boolean {
  return Date.now() < state.refreshSuspendUntil;
}

async function uploadFiles(files: File[]): Promise<void> {
  logClientEvent({
    category: "file",
    message: "file picker returned files",
    details: {
      fileCount: files.length,
      files: files.map((file) => file.name),
    },
  });
  state.isPickingFiles = false;
  const uploadableFiles = files.filter((file) => {
    const lowerName = file.name.toLowerCase();
    return lowerName.endsWith(".json") || lowerName.endsWith(".zip");
  });
  if (uploadableFiles.length === 0) {
    logClientEvent({
      category: "file",
      level: "warn",
      message: "no uploadable files selected",
      details: {
        fileCount: files.length,
      },
    });
    state.message = "Izaberi barem jedan .json ili .zip fajl.";
    state.messageTone = "danger";
    render();
    return;
  }

  const zipFiles = uploadableFiles.filter((file) =>
    file.name.toLowerCase().endsWith(".zip"),
  );

  state.isUploading = true;
  suspendMainRefresh(3000);
  state.message =
    zipFiles.length > 0 ? "Ucitavam katalog i raspakujem slike..." : "Ucitavam artikle...";
  state.messageTone = "muted";
  render();

  try {
    const formData = new FormData();
    for (const file of uploadableFiles) {
      formData.append("files", file);
    }

    const response = await fetch("/api/catalog/upload", {
      method: "POST",
      body: formData,
    });

    const data = (await response.json()) as CatalogResponse;
    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Upload nije uspeo.");
    }

    state.data = data as CatalogState;
    for (const item of state.data.items) {
      const imageKey = item.towKod || item.index || item.nameB2B;
      if (!(imageKey in state.imageIndexes)) {
        state.imageIndexes[imageKey] = 0;
      }
    }
    reconcileSelectionState();
    state.animateItemsIn = true;
    state.message = "Artikli su dodati.";
    state.messageTone = "success";
    logClientEvent({
      category: "file",
      message: "files uploaded successfully",
      details: {
        itemCount: state.data.totalItems,
        uploadedFiles: uploadableFiles.map((file) => file.name),
      },
    });
    window.setTimeout(() => {
      state.animateItemsIn = false;
      render();
    }, 220);
  } catch (error) {
    logClientEvent({
      category: "file",
      level: "error",
      message: "file upload failed",
      details: {
        error: error instanceof Error ? error.message : "Upload nije uspeo.",
        uploadedFiles: uploadableFiles.map((file) => file.name),
      },
    });
    state.message =
      error instanceof Error ? error.message : "Upload nije uspeo.";
    state.messageTone = "danger";
  } finally {
    state.isUploading = false;
    render();
  }
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function displayImagesForItem(item: CatalogItem): CatalogImage[] {
	return [...item.images].sort((left, right) => left.position - right.position);
}

function hasAIContent(item: CatalogItem, processing?: AIProcessingItem): boolean {
	return Boolean(item.aiTechnicalDescription?.trim()) || item.images.some((image) => image.source === "ai") || Boolean(processing?.image.status === "done" && item.processedImageUrl);
}

function renderAIProgressCounters(): string {
	const values = state.data.items.reduce((counts, item) => {
		const work = state.data.aiProcessing.items[item.itemId];
		for (const [kind, progress] of [["image", work?.image], ["description", work?.description]] as const) {
			if (!progress?.status) continue;
			counts[kind].requested++;
			if (progress.status === "done") counts[kind].done++;
			if (progress.status === "queued" || progress.status === "processing") counts[kind].active++;
			if (progress.status === "error") counts[kind].failed++;
		}
		return counts;
	}, { image: { requested: 0, done: 0, active: 0, failed: 0 }, description: { requested: 0, done: 0, active: 0, failed: 0 } });
	const counter = (label: string, value: { requested: number; done: number; active: number; failed: number }) => value.requested
		? `<span class="image-processing-progress"><b>${label}</b><strong class="${value.done > 0 ? "is-complete" : ""}">${value.done}</strong>/<span>${value.requested}</span>${value.active ? `<em>${value.active} u redu</em>` : ""}${value.failed ? `<em class="ai-count-error">${value.failed} greška</em>` : ""}</span>` : "";
	return counter("Slike", values.image) + counter("Opisi", values.description);
}

async function requestAIProcess(itemKeys: string[], kind: "image" | "description"): Promise<void> {
	if (itemKeys.length === 0) return;
	try {
		const response = await fetch("/api/ai-processing/run", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ itemKeys, kind }) });
		const data = await response.json() as AIProcessingState | ErrorResponse;
		if (!response.ok) throw new Error("error" in data ? data.error : "AI obrada nije pokrenuta.");
		state.data.aiProcessing = data as AIProcessingState;
		for (const item of state.data.items) if (itemKeys.includes(item.itemId)) item.useAIContent = true;
		state.message = kind === "image" ? "AI obrada slika je dodana u red." : "AI obrada opisa je dodana u red.";
		state.messageTone = "success";
	} catch (error) { state.message = error instanceof Error ? error.message : "AI obrada nije pokrenuta."; state.messageTone = "danger"; }
	render();
}

async function setAIContentToggle(itemID: string, enabled: boolean): Promise<void> {
	try {
		const response = await fetch(`/api/ai-processing/content?itemId=${encodeURIComponent(itemID)}`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ enabled }) });
		const data = await response.json() as CatalogState | ErrorResponse;
		if (!response.ok) throw new Error("error" in data ? data.error : "Promjena AI verzije nije uspjela.");
		state.data = data as CatalogState;
	} catch (error) { state.message = error instanceof Error ? error.message : "Promjena AI verzije nije uspjela."; state.messageTone = "danger"; }
	render();
}

function updateProductCardImage(
  item: CatalogItem,
  imageKey: string,
  images: CatalogImage[],
  imageIndex: number,
): void {
  const media = Array.from(document.querySelectorAll<HTMLElement>("[data-product-image-key]")).find(
    (element) => element.dataset.productImageKey === imageKey,
  );
  const imageElement = media?.querySelector<HTMLImageElement>("img");
  const countElement = document.querySelector<HTMLElement>(`[data-image-count-key="${CSS.escape(imageKey)}"]`);
  const next = images[imageIndex];
  if (!imageElement || !next) return;

  const preload = new Image();
  preload.onload = () => {
    if (state.imageIndexes[imageKey] !== imageIndex) return;
    imageElement.src = next.url;
    imageElement.alt = item.nameB2B || "Artikal";
    if (countElement) countElement.textContent = `${imageIndex + 1}/${images.length}`;
  };
  preload.src = next.url;
}

function renderAIProcessingStatuses(item: AIProcessingItem): string {
  const statuses: Array<{ label: string; tone: string }> = [];
  if (item.image.status === "processing") statuses.push({ label: "AI obrađuje sliku", tone: "processing" });
  else if (item.image.status === "queued") statuses.push({ label: "Slika čeka obradu", tone: "queued" });
  else if (item.image.status === "done") statuses.push({ label: "Slika gotova", tone: "done" });
  else if (item.image.status === "error") statuses.push({ label: item.image.error ? `Slika: ${item.image.error}` : "Slika nije obrađena", tone: "error" });

  if (item.description.status === "processing") statuses.push({ label: "AI obrađuje opis", tone: "processing" });
  else if (item.description.status === "queued") statuses.push({ label: "Opis čeka obradu", tone: "queued" });
  else if (item.description.status === "done") statuses.push({ label: "Opis gotov", tone: "done" });
  else if (item.description.status === "error") statuses.push({ label: item.description.error ? `Opis: ${item.description.error}` : "Opis nije obrađen", tone: "error" });

	return statuses.length ? `<div class="ai-processing-statuses">${statuses.map(({ label, tone }) => `<div class="image-processing-status ${escapeHtml(tone)}">${escapeHtml(label)}</div>`).join("")}</div>` : "";
}

function hasBlockingAIForCurrentSelection(): boolean {
  if (state.data.items.length === 0) return false;
  const selected = new Set(state.selectedTowKods);
  const targets = selected.size > 0
    ? state.data.items.filter((item) => selected.has(getItemSelectionKey(item)))
    : state.data.items;
	return targets.some((item) => {
		const work = state.data.aiProcessing.items[item.itemId];
		return work?.image.status === "queued" || work?.image.status === "processing" ||
			work?.description.status === "queued" || work?.description.status === "processing";
	});
}

function renderTechnicalDescription(item: CatalogItem): string {
	const ai = item.useAIContent && item.aiTechnicalDescription?.trim();
	const value = ai || item.technicalDescription || "Nema opisa.";
	if (!ai && item.technicalDescriptionFormat !== "html") {
    return `<p>${escapeHtml(value).replaceAll("\n", "<br>")}</p>`;
  }
  return sanitizeTechnicalDescriptionHTML(value);
}

function sanitizeTechnicalDescriptionHTML(value: string): string {
  const parsed = new DOMParser().parseFromString(value, "text/html");
  const allowed = new Set(["H3", "P", "STRONG", "EM", "UL", "OL", "LI", "BR"]);
  const clean = (node: Node): string => {
    if (node.nodeType === Node.TEXT_NODE) return escapeHtml(node.textContent || "");
    if (!(node instanceof HTMLElement)) return "";
    const children = Array.from(node.childNodes).map(clean).join("");
    if (!allowed.has(node.tagName)) return children;
    const tag = node.tagName.toLowerCase();
    return tag === "br" ? "<br>" : `<${tag}>${children}</${tag}>`;
  };
  return Array.from(parsed.body.childNodes).map(clean).join("");
}

function formatPriceValue(value: number): string {
  if (!Number.isFinite(value)) {
    return "0";
  }

  if (Number.isInteger(value)) {
    return String(value);
  }

  return value.toFixed(2).replace(/\.?0+$/, "");
}

function formatCurrencyCode(value: string): string {
  return value === "BAM" ? "KM" : value;
}

function loadStoredViewMode(): "grid" | "list" {
  const storedValue = window.localStorage.getItem(VIEW_MODE_STORAGE_KEY);
  return storedValue === "list" ? "list" : "grid";
}

function saveViewMode(value: "grid" | "list"): void {
  window.localStorage.setItem(VIEW_MODE_STORAGE_KEY, value);
}

function handleSortMenuOutsideClick(): void {
  if (!state.sortMenuOpen) {
    return;
  }

  state.sortMenuOpen = false;
  render();
}

function loadSessionSelectionMode(): boolean {
  return window.sessionStorage.getItem(SELECTION_MODE_STORAGE_KEY) === "true";
}

function saveStoredSelectionMode(value: boolean): void {
  window.sessionStorage.setItem(SELECTION_MODE_STORAGE_KEY, String(value));
}

function loadStoredSelectAllMode(): boolean {
  return window.localStorage.getItem(SELECT_ALL_STORAGE_KEY) === "true";
}

function saveStoredSelectAllMode(value: boolean): void {
  window.localStorage.setItem(SELECT_ALL_STORAGE_KEY, String(value));
}

function loadStoredSelectedTowKods(): string[] {
  const rawValue = window.localStorage.getItem(SELECTED_TOWKODS_STORAGE_KEY);
  if (!rawValue) {
    return [];
  }

  try {
    const parsed = JSON.parse(rawValue);
    return Array.isArray(parsed)
      ? parsed.filter((value): value is string => typeof value === "string" && value.length > 0)
      : [];
  } catch {
    return [];
  }
}

function saveStoredSelectedTowKods(values: string[]): void {
  window.localStorage.setItem(
    SELECTED_TOWKODS_STORAGE_KEY,
    JSON.stringify(values),
  );
}

async function exportCatalogArchive(): Promise<void> {
  const response = await fetch("/api/catalog/export", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      itemKeys: state.selectedTowKods,
    }),
  });

  if (!response.ok) {
    const data = (await response.json().catch(() => null)) as ErrorResponse | null;
    throw new Error(data?.error || "Cuvanje nije uspelo.");
  }

  const blob = await response.blob();
  const suggestedName =
    getSuggestedExportFileName(response.headers.get("Content-Disposition")) ||
    `ASI-${buildExportTimestamp()}.zip`;
  await saveBlobWithPicker(blob, suggestedName);
}

async function saveBlobWithPicker(blob: Blob, suggestedName: string): Promise<void> {
  const picker = (window as Window & {
    showSaveFilePicker?: (options?: {
      suggestedName?: string;
      types?: Array<{
        description?: string;
        accept: Record<string, string[]>;
      }>;
    }) => Promise<{
      createWritable: () => Promise<{
        write: (data: Blob) => Promise<void>;
        close: () => Promise<void>;
      }>;
    }>;
  }).showSaveFilePicker;

  if (picker) {
    const handle = await picker({
      suggestedName,
      types: [
        {
          description: "ZIP arhiva",
          accept: {
            "application/zip": [".zip"],
          },
        },
      ],
    });
    const writable = await handle.createWritable();
    await writable.write(blob);
    await writable.close();
    return;
  }

  const objectURL = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = objectURL;
  link.download = suggestedName;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 0);
}

function getSuggestedExportFileName(contentDisposition: string | null): string {
  if (!contentDisposition) {
    return "";
  }

  const utf8Match = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8Match?.[1]) {
    return decodeURIComponent(utf8Match[1]);
  }

  const plainMatch = contentDisposition.match(/filename="([^"]+)"/i);
  if (plainMatch?.[1]) {
    return plainMatch[1];
  }

  return "";
}

function buildExportTimestamp(): string {
  const now = new Date();
  const pad = (value: number): string => String(value).padStart(2, "0");
  return `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
}

function handleOlxTokenMenuOutsideClick(): void {
  if (!state.olxTokenMenuOpen) {
    return;
  }

  state.olxTokenMenuOpen = false;
  render();
}

function getPublishCandidateTowKods(): string[] {
  const hasSelection = state.selectedTowKods.length > 0;
  const selectedSet = new Set(state.selectedTowKods);

  return state.data.items
    .filter((item) => {
      const selectionKey = getItemSelectionKey(item);
      if (!selectionKey) {
        return false;
      }

      if (hasSelection) {
        return selectedSet.has(selectionKey);
      }

      return Boolean(item.categorization?.category?.id);
    })
    .map((item) => getItemSelectionKey(item));
}

function createPublishOptimisticSnapshot(): PublishOptimisticSnapshot {
  return {
    selectedTowKods: [...state.selectedTowKods],
    selectAllMode: state.selectAllMode,
    selectionPulseTowKods: [...state.selectionPulseTowKods],
  };
}

function applyOptimisticPublishRemoval(publishTowKods: string[]): void {
  const publishSet = new Set(publishTowKods);
  const nextItems = state.data.items.filter((item) => !publishSet.has(getItemSelectionKey(item)));

  state.data = {
    ...state.data,
    items: nextItems,
    totalItems: nextItems.length,
  };
  state.imageIndexes = filterImageIndexesByItems(state.imageIndexes, nextItems);
  state.selectedTowKods = state.selectedTowKods.filter((towKod) => !publishSet.has(towKod));
  state.selectionPulseTowKods = state.selectionPulseTowKods.filter((towKod) => !publishSet.has(towKod));
  reconcileSelectionState();
}

function restorePublishSnapshot(snapshot: PublishOptimisticSnapshot): void {
  state.selectedTowKods = snapshot.selectedTowKods;
  state.selectAllMode = snapshot.selectAllMode;
  state.selectionPulseTowKods = snapshot.selectionPulseTowKods;
  reconcileSelectionState();
}

async function reloadStateAfterPublishFailure(
  snapshot: PublishOptimisticSnapshot,
): Promise<boolean> {
  try {
    await syncCatalogStateFromBackend();
    restorePublishSnapshot(snapshot);
    state.message = "Objava nije uspela. Artikli su vraceni na stranicu.";
    state.messageTone = "danger";
    return true;
  } catch {
    restorePublishSnapshot(snapshot);
    return false;
  }
}

async function syncCatalogStateFromBackend(): Promise<void> {
  const response = await fetch("/api/catalog");
  const data = (await response.json()) as CatalogResponse;
  if (!response.ok) {
    throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
  }

  state.data = data as CatalogState;
  state.imageIndexes = filterImageIndexesByItems(state.imageIndexes, state.data.items);
  ensureImageIndexesForItems(state.data.items);
  state.selectedTowKods = state.selectedTowKods.filter((towKod) =>
    state.data.items.some((item) => getItemSelectionKey(item) === towKod),
  );
  reconcileSelectionState();
}

function showPublishOutcomeToasts(successCount: number, failedCount: number): void {
  if (successCount > 0) {
    notyf.success(`Uspjesno ste objavili ${successCount} artikala!`);
  }

  if (failedCount > 0) {
    notyf.error(`Neuspjesno ste objavili ${failedCount} artikala!`);
  }
}

function showPublishErrorToast(failedCount: number): void {
  const safeCount = Math.max(1, failedCount);
  notyf.error(`Neuspjesno ste objavili ${safeCount} artikala!`);
}

function showDailyLimitWarningToast(message: string): void {
  notyf.open({
    type: "warning",
    message: `<strong>${escapeHtml(message)}</strong>`,
  });
}

function extractDailyListingLimitMessage(error: unknown): string {
  const rawMessage =
    error instanceof Error ? error.message : typeof error === "string" ? error : "";
  const targetMessage = "Prekoračili ste limit objave oglasa od 350 po danu!";
  if (!rawMessage.includes(targetMessage)) {
    return "";
  }
  return targetMessage;
}

function getSelectableTowKods(items: CatalogItem[]): string[] {
  return items
    .map((item) => getItemSelectionKey(item))
    .filter((towKod): towKod is string => Boolean(towKod));
}

function reconcileSelectionState(): void {
  const selectableTowKods = new Set(getSelectableTowKods(state.data.items));
  state.selectedTowKods = state.selectedTowKods.filter((towKod) =>
    selectableTowKods.has(towKod),
  );
  state.selectAllMode =
    selectableTowKods.size > 0 &&
    state.selectedTowKods.length === selectableTowKods.size;
  saveStoredSelectedTowKods(state.selectedTowKods);
  saveStoredSelectAllMode(state.selectAllMode);
}

function toggleTowKodSelection(towKod: string): void {
  const isSelected = state.selectedTowKods.includes(towKod);
  state.selectedTowKods = isSelected
    ? state.selectedTowKods.filter((value) => value !== towKod)
    : [...state.selectedTowKods, towKod];
  state.selectionPulseTowKods = isSelected ? [] : [towKod];

  const selectableTowKods = getSelectableTowKods(state.data.items);
  state.selectAllMode =
    selectableTowKods.length > 0 &&
    state.selectedTowKods.length === selectableTowKods.length;
  saveStoredSelectedTowKods(state.selectedTowKods);
  saveStoredSelectAllMode(state.selectAllMode);
  if (!isSelected) {
    window.setTimeout(() => {
      state.selectionPulseTowKods = state.selectionPulseTowKods.filter(
        (value) => value !== towKod,
      );
      render();
    }, 360);
  }
}

function filterImageIndexesByItems(
  imageIndexes: Record<string, number>,
  items: CatalogItem[],
): Record<string, number> {
  const allowedKeys = new Set(
    items.map((item) => item.itemId),
  );

  return Object.fromEntries(
    Object.entries(imageIndexes).filter(([key]) => allowedKeys.has(key)),
  );
}

function ensureImageIndexesForItems(items: CatalogItem[]): void {
  for (const item of items) {
    const imageKey = item.itemId;
    if (!(imageKey in state.imageIndexes)) {
      state.imageIndexes[imageKey] = 0;
    }
  }
}

function loadStoredSortMode(): SortMode {
  const storedValue = window.localStorage.getItem(SORT_MODE_STORAGE_KEY);
  return isSortMode(storedValue) ? storedValue : "price-asc";
}

function saveSortMode(value: SortMode): void {
  window.localStorage.setItem(SORT_MODE_STORAGE_KEY, value);
}

function isSortMode(value: string | null): value is SortMode {
  return (
    value === "price-asc" ||
    value === "price-desc" ||
    value === "name" ||
    value === "brand" ||
    value === "publish-ready"
  );
}

function sortItems(items: CatalogItem[], sortMode: SortMode): CatalogItem[] {
  return [...items].sort((left, right) => compareCatalogItems(left, right, sortMode));
}

function compareCatalogItems(
  left: CatalogItem,
  right: CatalogItem,
  sortMode: SortMode,
): number {
  switch (sortMode) {
    case "price-asc":
      return compareNumbers(left.retailPrice, right.retailPrice) || compareByName(left, right);
    case "price-desc":
      return compareNumbers(right.retailPrice, left.retailPrice) || compareByName(left, right);
    case "brand":
      return compareText(left.brandName, right.brandName) || compareByName(left, right);
    case "publish-ready":
      return comparePublishReady(right, left) || compareByName(left, right);
    case "name":
      return compareByName(left, right);
  }
}

function comparePublishReady(left: CatalogItem, right: CatalogItem): number {
  const leftReady = left.categorization?.category?.id ? 1 : 0;
  const rightReady = right.categorization?.category?.id ? 1 : 0;
  return leftReady - rightReady;
}

function compareByName(left: CatalogItem, right: CatalogItem): number {
  return compareText(left.nameB2B, right.nameB2B);
}

function compareNumbers(left: number, right: number): number {
  const safeLeft = Number.isFinite(left) ? left : 0;
  const safeRight = Number.isFinite(right) ? right : 0;
  return safeLeft - safeRight;
}

function compareText(left: string, right: string): number {
  const safeLeft = left?.trim() || "";
  const safeRight = right?.trim() || "";
  return safeLeft.localeCompare(safeRight, "sr-Latn", {
    sensitivity: "base",
    numeric: true,
  });
}

function getEditTowKodFromPath(): string | null {
  const match = window.location.pathname.match(/^\/edit\/(.+)$/);
  if (!match) {
    return null;
  }

  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}

function isBulkCategoryPath(): boolean {
  return window.location.pathname === "/category-selected";
}

function isBulkEditPath(): boolean {
  return window.location.pathname === "/edit-selected";
}

function isDescriptionPath(): boolean {
  return window.location.pathname === "/description";
}

function getCategoryTowKodFromPath(): string | null {
  const match = window.location.pathname.match(/^\/category\/(.+)$/);
  if (!match) {
    return null;
  }

  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}

function isMainPath(): boolean {
  return window.location.pathname === "/";
}

function shouldPauseAuthPollingForCurrentPath(): boolean {
  return (
    isDescriptionPath() ||
    isBulkCategoryPath() ||
    isBulkEditPath() ||
    Boolean(getEditTowKodFromPath()) ||
    Boolean(getCategoryTowKodFromPath())
  );
}

window.addEventListener("popstate", () => {
  logClientEvent({
    category: "navigation",
    message: "popstate handled",
    details: {
      authenticated: state.isAuthenticated,
      path: window.location.pathname,
    },
  });
  if (!state.isAuthenticated) {
    render();
    return;
  }

  if (
    isDescriptionPath() ||
    isBulkCategoryPath() ||
    isBulkEditPath() ||
    Boolean(getEditTowKodFromPath()) ||
    Boolean(getCategoryTowKodFromPath())
  ) {
    render();
    return;
  }

  state.shouldRestoreMainScroll = true;
  render();
  void loadState();
});

window.addEventListener("pageshow", () => {
  logClientEvent({
    category: "window",
    message: "pageshow",
    details: {
      path: window.location.pathname,
    },
  });
  if (state.isPickingFiles) {
    window.setTimeout(() => {
      state.isPickingFiles = false;
    }, 300);
  }

  if (!state.isAuthenticated) {
    render();
    return;
  }

  refreshMainPageFromStore();
});

window.addEventListener("focus", () => {
  logClientEvent({
    category: "window",
    message: "window focus",
    details: {
      authenticated: state.isAuthenticated,
      path: window.location.pathname,
    },
  });
  if (!state.isAuthenticated) {
    void refreshAuthentication(true).then(() => {
      render();
    });
    return;
  }

  refreshMainPageFromStore();
});

window.addEventListener(
  "scroll",
  () => {
    if (!isMainPath()) {
      return;
    }

    saveMainScrollPosition();
  },
  { passive: true },
);

// Image generation runs server-side; polling keeps manual AI job statuses and
// progress counters live while the catalog page is open.
window.setInterval(() => {
  if (state.isAuthenticated && isMainPath()) {
    refreshMainPageFromStore(true);
  }
}, 1500);

async function initializeUpdatePrompt(): Promise<void> {
  // The backend checks once per ASI process. Polling here only waits for that
  // startup check to finish; it never schedules another update request.
  for (let attempt = 0; attempt < 20 && !updatePromptShown; attempt += 1) {
    try {
      const response = await fetch("/api/update/status", { cache: "no-store" });
      if (response.ok) {
        const status = (await response.json()) as UpdateStatus;
        if (status.checked) {
          showVersionToast(status.currentVersion);
          if (status.updated && status.version) {
            showUpdateComplete(status);
          }
          if (status.available && status.version && status.nonce) {
            showUpdatePrompt(status);
          }
          return;
        }
      }
    } catch {
      // Offline update checks must never block ASI startup.
    }
    await new Promise((resolve) => window.setTimeout(resolve, 500));
  }
}

function showVersionToast(version: string): void {
  if (versionToastShown) return;
  versionToastShown = true;
  notyf.open({
    type: "version",
    message: `<div class="version-toast"><strong>ASI verzija</strong><span>v${escapeHtml(version || "nepoznata")}</span></div>`,
  });
}

function showUpdateComplete(status: UpdateStatus): void {
  notyf.success(`ASI je ažuriran na v${status.version || ""}.${status.notes ? ` ${status.notes}` : ""}`);
}

function showUpdatePrompt(status: UpdateStatus): void {
  if (updatePromptShown) {
    return;
  }
  updatePromptShown = true;
  const toastID = `update-${status.nonce}`;
  notyf.open({
    type: "update",
    message: `<div class="update-toast" data-update-toast="${escapeHtml(toastID)}"><strong>Dostupno je ažuriranje v${escapeHtml(status.version || "")}</strong><span>${status.notes ? escapeHtml(status.notes) : "Da li želite ažurirati aplikaciju?"}</span><div class="update-toast-actions"><button type="button" data-update-decline="${escapeHtml(toastID)}">Ne</button><button type="button" data-update-accept="${escapeHtml(toastID)}">Da, ažuriraj</button></div></div>`,
  });
  const decline = document.querySelector<HTMLButtonElement>(`[data-update-decline="${CSS.escape(toastID)}"]`);
  const accept = document.querySelector<HTMLButtonElement>(`[data-update-accept="${CSS.escape(toastID)}"]`);
  decline?.addEventListener("click", async () => {
    await fetch("/api/update/decline", {
      method: "POST",
      headers: { "X-ASI-Update-Nonce": status.nonce },
    }).catch(() => undefined);
    decline?.closest(".notyf__toast")?.remove();
  });
  accept?.addEventListener("click", async () => {
    if (!accept || !decline) return;
    accept.disabled = true;
    decline.disabled = true;
    accept.textContent = "Pokrećem updater...";
    try {
      const response = await fetch("/api/update/accept", {
        method: "POST",
        headers: { "X-ASI-Update-Nonce": status.nonce },
      });
      if (!response.ok) throw new Error("Updater nije pokrenut.");
      accept.closest(".notyf__toast")?.remove();
    } catch (error) {
      accept.disabled = false;
      decline.disabled = false;
      accept.textContent = "Da, ažuriraj";
      notyf.error(error instanceof Error ? error.message : "Updater nije pokrenut.");
    }
  });
}

void (async () => {
  void initializeUpdatePrompt();
  await refreshAuthentication(true);
  await startApplication();
})();
