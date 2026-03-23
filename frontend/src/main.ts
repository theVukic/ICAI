import "./style.css";
import { mountBulkCategoryPage } from "./bulk-category";
import { mountCategoryPage } from "./category";
import { mountBulkEditPage } from "./bulk-edit";
import { mountEditPage } from "./edit";
import { loadSessionInfo, renderSessionBadge } from "./session";
import { mountPageTransition, navigateWithFade } from "./transition";

type CatalogImage = {
  position: number;
  url: string;
};

type CatalogStatus = {
  code: string;
  name: string;
};

type CatalogItem = {
  towKod: string;
  index: string;
  nameB2B: string;
  brandName: string;
  images: CatalogImage[];
  technicalDescription: string;
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
};

type ErrorResponse = {
  error: string;
};

type CatalogResponse = CatalogState | ErrorResponse;

const VIEW_MODE_STORAGE_KEY = "icaiapp:view-mode";
const SORT_MODE_STORAGE_KEY = "icaiapp:sort-mode";
const SELECTION_MODE_STORAGE_KEY = "icaiapp:selection-mode";
const SELECT_ALL_STORAGE_KEY = "icaiapp:select-all";
const SELECTED_TOWKODS_STORAGE_KEY = "icaiapp:selected-towkods";

type SortMode = "price-asc" | "price-desc" | "name" | "brand";

const SORT_MODE_LABELS: Record<SortMode, string> = {
  "price-asc": "Cijena najniza",
  "price-desc": "Cijena najvisa",
  name: "Ime",
  brand: "Brend",
};

const app = document.querySelector<HTMLDivElement>("#app");

if (!app) {
  throw new Error("No #app element found.");
}

const root = app;
mountPageTransition(root);

const state: {
  data: CatalogState;
  isDragging: boolean;
  isUploading: boolean;
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
  selectionPulseTowKods: string[];
  animateItemsIn: boolean;
  sessionUsername: string;
  tokenConfigured: boolean;
} = {
  data: {
    catalogs: [],
    items: [],
    tempDir: "",
    totalFiles: 0,
    totalItems: 0,
    lastUpdated: null,
  },
  isDragging: false,
  isUploading: false,
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
  selectionPulseTowKods: [],
  animateItemsIn: false,
  sessionUsername: "",
  tokenConfigured: false,
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

function render(): void {
  if (isBulkCategoryPath()) {
    mountBulkCategoryPage(root);
    return;
  }

  if (isBulkEditPath()) {
    mountBulkEditPage(root);
    return;
  }

  const editTowKod = getEditTowKodFromPath();
  if (editTowKod) {
    mountEditPage(root, editTowKod);
    return;
  }

  const categoryTowKod = getCategoryTowKodFromPath();
  if (categoryTowKod) {
    mountCategoryPage(root, categoryTowKod);
    return;
  }

  const hasItems = state.data.items.length > 0;

  const sortedItems = sortItems(state.data.items, state.sortMode);

  const cards = sortedItems
    .map((item) => {
      const imageKey = item.towKod || item.index || item.nameB2B;
      const isSelected = item.towKod
        ? state.selectedTowKods.includes(item.towKod)
        : false;
      const shouldPulse = item.towKod
        ? state.selectionPulseTowKods.includes(item.towKod)
        : false;
      const currentImageIndex = Math.min(
        state.imageIndexes[imageKey] ?? 0,
        Math.max(item.images.length - 1, 0),
      );
      const image = item.images[currentImageIndex]?.url ?? "";
      const price =
        item.retailPrice > 0
          ? `${formatPriceValue(item.retailPrice)} ${escapeHtml(formatCurrencyCode(item.currencyCode || "EUR"))}`
          : "Na upit";
      const hasCategorization = Boolean(item.categorization?.category?.id);

      return `
        <article class="product-card ${state.viewMode === "list" ? "list-mode" : ""} ${state.selectionMode ? "selection-enabled" : ""} ${isSelected ? "is-selected" : ""} ${shouldPulse ? "selection-pulse" : ""}" data-select-card="${escapeHtml(item.towKod || "")}">
          <div class="product-media">
            ${
              isSelected
                ? `<div class="selection-badge">ODABRANO</div>`
                : ""
            }
            ${
              image
                ? `<img src="${image}" alt="${escapeHtml(item.nameB2B)}" loading="lazy" draggable="false" />`
                : `<div class="product-placeholder">Nema slike</div>`
            }
            ${
              item.images.length > 1
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
              item.images.length > 0
                ? `<div class="image-count">${currentImageIndex + 1}/${item.images.length}</div>`
                : ""
            }
          </div>
          <div class="product-body">
            <h3>${escapeHtml(item.nameB2B || "Artikal bez naziva")}</h3>
            <div class="product-line">
              <span>${escapeHtml(item.brandName || "Nepoznat brend")}</span>
              <span>${escapeHtml(item.index || item.towKod || "Bez sifre")}</span>
            </div>
            <p>${escapeHtml(item.technicalDescription || "Nema opisa.")}</p>
            <div class="product-line bottom">
              <strong>${price}</strong>
              <div class="card-actions">
                <button class="icon-button ${hasCategorization ? "category-button-complete" : "category-button-missing"}" type="button" aria-label="Kategorizacija" data-category-code="${escapeHtml(item.towKod)}">
                  ${categorizeIcon}
                </button>
                <button class="icon-button" type="button" aria-label="Promjeni" data-edit-code="${escapeHtml(item.towKod)}">
                  ${editIcon}
                </button>
              </div>
            </div>
          </div>
        </article>
      `;
    })
    .join("");

  root.innerHTML = `
    <div class="shell">
      <header class="topbar">
        <div class="logo-wrap">
          <div class="logo-mark">IC</div>
          <strong>ICAI</strong>
          ${renderSessionBadge(state.tokenConfigured ? { tokenConfigured: true, username: state.sessionUsername, fullName: "" } : null)}
        </div>
        <div class="header-actions">
        ${
          state.selectionToolsVisible
            ? `
              <div class="selection-tools ${state.selectionToolsAnimation}">
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
        </div>
      </header>

      <main class="main-layout ${hasItems ? "has-items" : "is-empty"}">
        <section class="drop-layer ${state.isDragging ? "dragging" : ""}" id="dropZone">
        <input id="fileInput" type="file" accept=".json,application/json" multiple hidden />

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
        <div class="toolbar-action">
          <button class="danger-button" id="clearButton">
            <span class="button-icon">${deleteIcon}</span>
          </button>
          <span class="action-label">OBRISI</span>
        </div>
        <div class="toolbar-action">
          <button class="publish-button" type="button">
            <span class="button-icon">${publishIcon}</span>
          </button>
          <span class="action-label">OBJAVI</span>
        </div>
      </footer>
    </div>
  `;

  bindEvents();
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
  const selectableCards =
    document.querySelectorAll<HTMLElement>("[data-select-card]");
  const sortMenu = document.querySelector<HTMLElement>("#sortMenu");
  const sortOptionButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-sort-mode]");
  const browseButton =
    document.querySelector<HTMLButtonElement>("#browseButton");
  const dropZone = document.querySelector<HTMLElement>("#dropZone");
  const clearButton = document.querySelector<HTMLButtonElement>("#clearButton");
  const imageButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-image-nav]");
  const editButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-edit-code]");
  const categoryButtons =
    document.querySelectorAll<HTMLButtonElement>("[data-category-code]");

  if (!fileInput || !uploadButton || !clearButton) {
    return;
  }

  const openPicker = () => fileInput.click();
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

    navigateWithFade(root, "/edit-selected");
  });
  categorySelectedButton?.addEventListener("click", () => {
    if (state.selectedTowKods.length === 0) {
      return;
    }

    navigateWithFade(root, "/category-selected");
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

      const item = state.data.items.find(
        (entry) => (entry.towKod || entry.index || entry.nameB2B) === imageKey,
      );

      if (!item || item.images.length < 2) {
        return;
      }

      const currentIndex = state.imageIndexes[imageKey] ?? 0;
      const delta = direction === "next" ? 1 : -1;
      const nextIndex =
        (currentIndex + delta + item.images.length) % item.images.length;
      state.imageIndexes[imageKey] = nextIndex;
      render();
    });
  }

  for (const button of editButtons) {
    button.addEventListener("click", () => {
      const code = button.dataset.editCode;
      if (!code) {
        return;
      }

      navigateWithFade(root, `/edit/${encodeURIComponent(code)}`);
    });
  }

  for (const button of categoryButtons) {
    button.addEventListener("click", () => {
      const code = button.dataset.categoryCode;
      if (!code) {
        return;
      }

      navigateWithFade(root, `/category/${encodeURIComponent(code)}`);
    });
  }
}

async function loadState(): Promise<void> {
  try {
    const session = await loadSessionInfo();
    state.sessionUsername = session.username || "";
    state.tokenConfigured = session.tokenConfigured;

    const response = await fetch("/api/catalog");
    const data = (await response.json()) as CatalogResponse;

    if (!response.ok) {
      throw new Error("error" in data ? data.error : "Ucitavanje nije uspelo.");
    }

    state.data = data as CatalogState;
    state.imageIndexes = {};
    reconcileSelectionState();
    if (state.data.totalItems > 0) {
      state.message = "Artikli su ucitani.";
      state.messageTone = "success";
    }
  } catch (error) {
    state.message =
      error instanceof Error ? error.message : "Backend nije dostupan.";
    state.messageTone = "danger";
  } finally {
    render();
  }
}

function refreshMainPageFromStore(): void {
  if (getEditTowKodFromPath()) {
    return;
  }

  void loadState();
}

async function uploadFiles(files: File[]): Promise<void> {
  const jsonFiles = files.filter((file) =>
    file.name.toLowerCase().endsWith(".json"),
  );
  if (jsonFiles.length === 0) {
    state.message = "Izaberi barem jedan .json fajl.";
    state.messageTone = "danger";
    render();
    return;
  }

  state.isUploading = true;
  state.message = "Ucitavam artikle...";
  state.messageTone = "muted";
  render();

  try {
    const formData = new FormData();
    for (const file of jsonFiles) {
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
    window.setTimeout(() => {
      state.animateItemsIn = false;
      render();
    }, 220);
  } catch (error) {
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

function getSelectableTowKods(items: CatalogItem[]): string[] {
  return items
    .map((item) => item.towKod)
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
    value === "brand"
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
    case "name":
      return compareByName(left, right);
  }
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

if (isBulkCategoryPath()) {
  mountBulkCategoryPage(root);
} else if (isBulkEditPath()) {
  mountBulkEditPage(root);
} else {
  const editTowKod = getEditTowKodFromPath();
  if (editTowKod) {
    mountEditPage(root, editTowKod);
  } else {
    const categoryTowKod = getCategoryTowKodFromPath();
    if (categoryTowKod) {
      mountCategoryPage(root, categoryTowKod);
    } else {
      void loadState();
    }
  }
}

window.addEventListener("pageshow", () => {
  refreshMainPageFromStore();
});

window.addEventListener("focus", () => {
  refreshMainPageFromStore();
});
